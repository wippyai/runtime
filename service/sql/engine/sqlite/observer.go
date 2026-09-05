// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mattn/go-sqlite3"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

var (
	errObserverClosed    = errors.New("sqlite mutation observer is closed")
	errObserverOverflow  = errors.New("sqlite mutation observer backlog overflow")
	errObserverAmbiguous = errors.New("sqlite mutation observer cannot determine statement outcome")
)

const (
	defaultSnapshotBatchSize = 512
	maxSnapshotBatchSize     = 4096
	mutationStructuralBytes  = 128
	valueStructuralBytes     = 24
)

// sqliteConnector keeps the SQLite driver and its connection state owned by
// one SQL pool. It intentionally uses sql.OpenDB instead of sql.Register, so
// no process-global driver name or file-path registry is involved.
type sqliteConnector struct {
	driver  *sqlite3.SQLiteDriver
	backend *sqliteBackend
	dsn     string
}

func (c *sqliteConnector) Connect(context.Context) (driver.Conn, error) {
	raw, err := c.driver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	sqliteConn, ok := raw.(*sqlite3.SQLiteConn)
	if !ok {
		_ = raw.Close()
		return nil, fmt.Errorf("sqlite driver returned %T", raw)
	}

	conn := &observedConn{
		raw:     raw,
		sqlite:  sqliteConn,
		backend: c.backend,
		state: &sqliteConnectionState{
			backend: c.backend, sqlite: sqliteConn,
			maxChanges: c.backend.maxChanges, maxBytes: c.backend.maxBytes,
			maxCommitEnds: c.backend.maxChanges,
		},
	}
	// Install hooks for every physical connection when it is created. This
	// avoids trying to mutate a connection that may be in use when a stream is
	// subscribed; the backend decides whether candidates are retained.
	conn.bindIfActive()
	return conn, nil
}

func (c *sqliteConnector) Driver() driver.Driver { return c.driver }

func openSQLite(_ context.Context, dsn string, limits ...int) (*sql.DB, sqlapi.CommittedMutationSource, error) {
	maxChanges, maxBytes := observerLimits(limits)
	backend := newSQLiteBackend(maxChanges, maxBytes)
	connector := &sqliteConnector{
		dsn:     dsn,
		driver:  &sqlite3.SQLiteDriver{},
		backend: backend,
	}
	db := sql.OpenDB(connector)
	backend.db = db
	return db, backend, nil
}

type sqliteBackend struct {
	relayWake    chan struct{}
	streams      map[*mutationStream]struct{}
	fence        chan struct{}
	db           *sql.DB
	relayDone    chan struct{}
	relayQueue   []*backendBatch
	maxChanges   int
	sequence     atomic.Uint64
	maxBytes     int
	relayChanges int
	relayBytes   int
	mu           sync.Mutex
	closed       bool
}

type backendBatch struct {
	streams []*mutationStream
	batch   sqlapi.MutationBatch
	bytes   int
}

func newSQLiteBackend(maxChanges, maxBytes int) *sqliteBackend {
	fence := make(chan struct{}, 1)
	fence <- struct{}{}
	backend := &sqliteBackend{
		streams:    make(map[*mutationStream]struct{}),
		fence:      fence,
		maxChanges: maxChanges,
		maxBytes:   maxBytes,
		relayWake:  make(chan struct{}, 1),
		relayDone:  make(chan struct{}),
	}
	go func() {
		defer close(backend.relayDone)
		backend.relay()
	}()
	return backend
}

func observerLimits(limits []int) (int, int) {
	maxChanges, maxBytes := sqlapi.DefaultMaxMutationChanges, sqlapi.DefaultMaxMutationBytes
	if len(limits) > 0 && limits[0] > 0 {
		maxChanges = limits[0]
	}
	if len(limits) > 1 && limits[1] > 0 {
		maxBytes = limits[1]
	}
	return maxChanges, maxBytes
}

func (b *sqliteBackend) acquireFence(ctx context.Context) error {
	select {
	case <-b.fence:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *sqliteBackend) acquireCommitFence() {
	<-b.fence
}

func (b *sqliteBackend) releaseFence() {
	b.fence <- struct{}{}
}

func (b *sqliteBackend) hasObservers() bool {
	b.mu.Lock()
	active := !b.closed && len(b.streams) > 0
	b.mu.Unlock()
	return active
}

func (b *sqliteBackend) Subscribe(ctx context.Context, opts sqlapi.MutationOptions) (sqlapi.MutationStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.MaxChanges <= 0 {
		opts.MaxChanges = b.maxChanges
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = b.maxBytes
	}
	if err := b.validateTables(ctx, opts.Tables); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errObserverClosed
	}
	stream := newMutationStream(ctx, b, opts)
	b.streams[stream] = struct{}{}
	b.mu.Unlock()
	stream.start()
	return stream, nil
}

func (b *sqliteBackend) validateTables(ctx context.Context, requested []string) error {
	b.mu.Lock()
	db := b.db
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errObserverClosed
	}
	if db == nil {
		return errors.New("sqlite observer has no database")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire sqlite observer connection: %w", err)
	}
	defer conn.Close()
	return conn.Raw(func(raw any) error {
		observed, ok := raw.(*observedConn)
		if !ok {
			return fmt.Errorf("sqlite observer received %T", raw)
		}
		state := &sqliteConnectionState{backend: b, sqlite: observed.sqlite}
		tables, err := tablesForValidation(observed.sqlite, requested)
		if err != nil {
			return err
		}
		for _, table := range tables {
			if err := state.validateTable(table.schema, table.name); err != nil {
				return err
			}
		}
		return nil
	})
}

func tablesForValidation(conn *sqlite3.SQLiteConn, requested []string) ([]snapshotTable, error) {
	if len(requested) == 0 {
		rows, err := conn.Query(`SELECT name FROM main.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`, nil)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var tables []snapshotTable
		values := make([]driver.Value, len(rows.Columns()))
		for {
			err := rows.Next(values)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			table, ok := values[0].(string)
			if !ok {
				return nil, fmt.Errorf("sqlite table name has type %T", values[0])
			}
			tables = append(tables, snapshotTable{schema: "main", name: table})
		}
		return tables, nil
	}
	tables := make([]snapshotTable, 0, len(requested))
	for _, name := range requested {
		parts := strings.SplitN(name, ".", 2)
		if len(parts) == 1 {
			tables = append(tables, snapshotTable{schema: "main", name: parts[0]})
		} else {
			tables = append(tables, snapshotTable{schema: parts[0], name: parts[1]})
		}
	}
	return tables, nil
}

func (b *sqliteBackend) remove(stream *mutationStream, err error) {
	b.mu.Lock()
	delete(b.streams, stream)
	b.mu.Unlock()

	stream.closeWithError(err)
}

func (b *sqliteBackend) publish(changes []sqlapi.Mutation) {
	if len(changes) == 0 {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if len(b.streams) == 0 {
		b.mu.Unlock()
		return
	}
	streams := make([]*mutationStream, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	changes = append([]sqlapi.Mutation(nil), changes...)
	sequence := b.sequence.Add(1)
	batch := sqlapi.MutationBatch{
		Transaction: strconv.FormatUint(sequence, 10),
		Changes:     changes,
	}
	batchBytes := mutationBatchBytes(batch)
	if (b.maxChanges > 0 && (len(changes) > b.maxChanges || b.relayChanges > b.maxChanges-len(changes))) ||
		(b.maxBytes > 0 && (batchBytes > b.maxBytes || b.relayBytes > b.maxBytes-batchBytes)) {
		b.closed = true
		b.streams = make(map[*mutationStream]struct{})
		b.relayQueue = nil
		b.relayChanges = 0
		b.relayBytes = 0
		b.mu.Unlock()
		b.closeStreams(streams, errObserverOverflow)
		b.signalRelay()
		return
	}
	b.relayQueue = append(b.relayQueue, &backendBatch{batch: batch, streams: streams, bytes: batchBytes})
	b.relayChanges = saturatingAdd(b.relayChanges, len(changes))
	b.relayBytes = saturatingAdd(b.relayBytes, batchBytes)
	b.mu.Unlock()
	b.signalRelay()
}

func (b *sqliteBackend) fail(err error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	streams := make([]*mutationStream, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	b.streams = make(map[*mutationStream]struct{})
	b.relayQueue = nil
	b.relayChanges = 0
	b.relayBytes = 0
	b.mu.Unlock()

	b.closeStreams(streams, err)
	b.signalRelay()
}

func (b *sqliteBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		<-b.relayDone
		return nil
	}
	b.closed = true
	streams := make([]*mutationStream, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	b.streams = make(map[*mutationStream]struct{})
	b.relayQueue = nil
	b.relayChanges = 0
	b.relayBytes = 0
	b.mu.Unlock()

	b.closeStreams(streams, errObserverClosed)
	b.signalRelay()
	<-b.relayDone
	return nil
}

func (b *sqliteBackend) closeStreams(streams []*mutationStream, err error) {
	for _, stream := range streams {
		stream.closeWithError(err)
	}
}

func (b *sqliteBackend) signalRelay() {
	select {
	case b.relayWake <- struct{}{}:
	default:
	}
}

func (b *sqliteBackend) relay() {
	for {
		b.mu.Lock()
		if len(b.relayQueue) == 0 {
			closed := b.closed
			b.mu.Unlock()
			if closed {
				return
			}
			<-b.relayWake
			continue
		}
		item := b.relayQueue[0]
		b.mu.Unlock()

		for _, stream := range item.streams {
			stream.push(item.batch)
		}

		b.mu.Lock()
		if len(b.relayQueue) > 0 && b.relayQueue[0] == item {
			b.relayQueue = b.relayQueue[1:]
			b.relayChanges -= len(item.batch.Changes)
			b.relayBytes -= item.bytes
		}
		b.mu.Unlock()
	}
}

func scanSQLiteRow(data *sqlite3.SQLitePreUpdateData, count int, isNew bool) ([]any, error) {
	if count <= 0 {
		return nil, nil
	}
	values := make([]any, count)
	var err error
	if isNew {
		err = data.New(values...)
	} else {
		err = data.Old(values...)
	}
	return values, err
}

func normalizeSavepointName(name string) string {
	// The authorizer already supplies the decoded identifier, not SQL text.
	// SQLite identifier comparison folds ASCII only; spaces and quotes within
	// the identifier are significant, as are non-ASCII case differences.
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, name)
}

var _ driver.Connector = (*sqliteConnector)(nil)
var _ driver.Conn = (*observedConn)(nil)
var _ driver.Tx = (*observedTx)(nil)
var _ driver.Stmt = (*observedStmt)(nil)
var _ driver.Rows = (*observedRows)(nil)
var _ sqlapi.CommittedMutationSource = (*sqliteBackend)(nil)
var _ sqlapi.MutationStream = (*mutationStream)(nil)
