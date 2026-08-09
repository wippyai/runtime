// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
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
	dsn     string
	driver  *sqlite3.SQLiteDriver
	backend *sqliteBackend
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
	db         *sql.DB
	streams    map[*mutationStream]struct{}
	mu         sync.Mutex
	fence      chan struct{}
	maxChanges int
	maxBytes   int
	closed     bool
	sequence   atomic.Uint64
}

func newSQLiteBackend(maxChanges, maxBytes int) *sqliteBackend {
	fence := make(chan struct{}, 1)
	fence <- struct{}{}
	return &sqliteBackend{streams: make(map[*mutationStream]struct{}), fence: fence, maxChanges: maxChanges, maxBytes: maxBytes}
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
	active := !b.closed
	if active {
		active = false
		for stream := range b.streams {
			stream.mu.Lock()
			closed := stream.closed
			stream.mu.Unlock()
			if !closed {
				active = true
				break
			}
		}
	}
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
	stream := newMutationStream(b, opts)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		stream.closeWithError(errObserverClosed)
		return nil, errObserverClosed
	}
	b.streams[stream] = struct{}{}
	b.mu.Unlock()
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
			if err == io.EOF {
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

func (b *sqliteBackend) Snapshot(ctx context.Context, opts sqlapi.SnapshotOptions) (sqlapi.SnapshotStream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	closed := b.closed
	db := b.db
	b.mu.Unlock()
	if closed {
		return nil, errObserverClosed
	}
	if db == nil {
		return nil, errors.New("sqlite snapshot has no database")
	}
	// Reserve the physical connection before taking the fence. SQL pools may
	// have a single connection; taking the fence first would let a writer hold
	// that connection while waiting for the snapshot and deadlock both paths.
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire sqlite snapshot connection: %w", err)
	}
	if err := b.acquireFence(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	release := true
	defer func() {
		if release {
			b.releaseFence()
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("begin sqlite snapshot: %w", err)
	}
	// database/sql may defer BEGIN until the first operation. Force a read
	// while the fence is held so the transaction's SQLite read view is fixed
	// before later commits can be buffered as live changes.
	var schemaVersion int64
	if err := tx.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaVersion); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, fmt.Errorf("establish sqlite snapshot view: %w", err)
	}
	if err := validateSnapshotTablesTx(ctx, tx, opts.Tables); err != nil {
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, err
	}
	watermark := strconv.FormatUint(b.sequence.Load(), 10)
	scanCtx, cancel := context.WithCancel(ctx)
	if opts.MaxChanges <= 0 {
		opts.MaxChanges = b.maxChanges
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = b.maxBytes
	}
	stream := newSnapshotStream(b, opts, watermark, cancel)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, errObserverClosed
	}
	b.streams[stream] = struct{}{}
	b.mu.Unlock()
	// The fence remains held until the stream is registered and its read view
	// has been established. New commits therefore receive a sequence greater
	// than watermark and are buffered by this stream.
	release = false
	b.releaseFence()
	go b.scanSnapshot(scanCtx, conn, tx, stream, opts)
	return stream, nil
}

func (b *sqliteBackend) scanSnapshot(ctx context.Context, conn *sql.Conn, tx *sql.Tx, stream *mutationStream, opts sqlapi.SnapshotOptions) {
	defer conn.Close()
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			stream.finishSnapshot(err)
		}
	}()
	tables, err := snapshotTables(ctx, tx, opts.Tables)
	if err == nil {
		batchSize := normalizeSnapshotBatchSize(opts.BatchSize)
		for _, table := range tables {
			if err = scanSnapshotTable(ctx, tx, stream, table.schema, table.name, batchSize); err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = tx.Rollback()
		b.remove(stream, err)
		return
	}
	if err = tx.Commit(); err != nil {
		b.remove(stream, fmt.Errorf("commit sqlite snapshot: %w", err))
		return
	}
	stream.finishSnapshot(nil)
}

func normalizeSnapshotBatchSize(value int) int {
	if value <= 0 {
		return defaultSnapshotBatchSize
	}
	if value > maxSnapshotBatchSize {
		return maxSnapshotBatchSize
	}
	return value
}

type snapshotTable struct {
	schema string
	name   string
}

func snapshotTables(ctx context.Context, tx *sql.Tx, requested []string) ([]snapshotTable, error) {
	if len(requested) == 0 {
		rows, err := tx.QueryContext(ctx, `SELECT name FROM main.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var tables []snapshotTable
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			tables = append(tables, snapshotTable{schema: "main", name: name})
		}
		return tables, rows.Err()
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

func validateSnapshotTablesTx(ctx context.Context, tx *sql.Tx, requested []string) error {
	tables, err := snapshotTables(ctx, tx, requested)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if strings.EqualFold(table.schema, "temp") {
			return errors.New("sqlite snapshot does not support TEMP tables")
		}
		var definition sql.NullString
		query := fmt.Sprintf("SELECT sql FROM %s.sqlite_master WHERE type = 'table' AND name = ?", quoteIdentifier(table.schema))
		if err := tx.QueryRowContext(ctx, query, table.name).Scan(&definition); err != nil {
			return fmt.Errorf("inspect sqlite snapshot table %s.%s: %w", table.schema, table.name, err)
		}
		upper := strings.ToUpper(definition.String)
		if strings.Contains(upper, "WITHOUT ROWID") {
			return fmt.Errorf("sqlite snapshot does not support WITHOUT ROWID table %s.%s", table.schema, table.name)
		}
		if strings.HasPrefix(strings.TrimSpace(upper), "CREATE VIRTUAL TABLE") {
			return fmt.Errorf("sqlite snapshot does not support virtual table %s.%s", table.schema, table.name)
		}
	}
	return nil
}

func scanSnapshotTable(ctx context.Context, tx *sql.Tx, stream *mutationStream, schema, table string, batchSize int) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT rowid, * FROM %s.%s", quoteIdentifier(schema), quoteIdentifier(table)))
	if err != nil {
		return fmt.Errorf("scan sqlite snapshot %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	columns = append([]string(nil), columns[1:]...)
	changes := make([]sqlapi.Mutation, 0, batchSize)
	for rows.Next() {
		values := make([]any, len(columns)+1)
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		rowID, ok := values[0].(int64)
		if !ok || rowID == 0 {
			return fmt.Errorf("sqlite snapshot %s.%s returned invalid rowid %v", schema, table, values[0])
		}
		after := append([]any(nil), values[1:]...)
		changes = append(changes, sqlapi.Mutation{
			Schema: schema, Table: table, Columns: columns,
			RowID: rowID, After: after, Op: "snapshot",
		})
		if len(changes) >= batchSize {
			if err := stream.pushSnapshot(sqlapi.MutationBatch{Transaction: stream.watermark, Snapshot: true, Changes: append([]sqlapi.Mutation(nil), changes...)}); err != nil {
				return err
			}
			changes = changes[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(changes) > 0 {
		return stream.pushSnapshot(sqlapi.MutationBatch{Transaction: stream.watermark, Snapshot: true, Changes: append([]sqlapi.Mutation(nil), changes...)})
	}
	return nil
}

func (b *sqliteBackend) remove(stream *mutationStream, err error) {
	b.mu.Lock()
	if _, ok := b.streams[stream]; ok {
		delete(b.streams, stream)
	}
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
	streams := make([]*mutationStream, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	sequence := b.sequence.Add(1)
	b.mu.Unlock()

	batch := sqlapi.MutationBatch{
		Transaction: strconv.FormatUint(sequence, 10),
		Changes:     changes,
	}
	for _, stream := range streams {
		stream.push(batch)
	}
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
	b.mu.Unlock()

	for _, stream := range streams {
		stream.closeWithError(err)
	}
}

func (b *sqliteBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	streams := make([]*mutationStream, 0, len(b.streams))
	for stream := range b.streams {
		streams = append(streams, stream)
	}
	b.streams = make(map[*mutationStream]struct{})
	b.mu.Unlock()

	for _, stream := range streams {
		stream.closeWithError(errObserverClosed)
	}
	return nil
}

// sqliteConnectionState is attached to one physical SQLite connection. The
// hooks only collect a candidate transaction. Publication happens from the
// driver wrappers after Exec/Commit/Rows completion, when statement rollback
// and savepoint effects are known.
type sqliteConnectionState struct {
	backend                *sqliteBackend
	sqlite                 *sqlite3.SQLiteConn
	pending                []sqlapi.Mutation
	pendingBytes           int
	maxChanges             int
	maxBytes               int
	savepoints             []savepoint
	statementMark          int
	statementSavepointVerb string
	statementSavepointName string
	rollbackSeen           bool
	rollbackUnconfirmed    bool
	commitPending          bool
	commitEnds             []int
	confirmedEnds          int
	fenceHeld              bool
	ddlInTxn               bool
	dmlInTxn               bool
	failed                 error
	statementDDL           bool
	unsupported            error
	prepareMeta            statementMeta
}

// statementMeta is collected by SQLite's authorizer while a statement is
// prepared. It avoids interpreting comments, literals, or quoted identifiers
// as executable SQL control words. A prepared statement carries this metadata
// to its later execution; direct Exec/Query paths merge it after the driver's
// native prepare loop returns.
type statementMeta struct {
	ddl            bool
	unsupported    error
	savepointVerb  string
	savepointName  string
	savepointCount int
}

type savepoint struct {
	name  string
	index int
}

func (s *sqliteConnectionState) bind(conn *sqlite3.SQLiteConn) {
	conn.RegisterPreUpdateHook(s.preUpdate)
	conn.RegisterCommitHook(s.commit)
	conn.RegisterRollbackHook(s.rollback)
	conn.RegisterAuthorizer(s.authorizer)
	s.sqlite = conn
}

func (s *sqliteConnectionState) clear(conn *sqlite3.SQLiteConn) {
	conn.RegisterPreUpdateHook(nil)
	conn.RegisterCommitHook(nil)
	conn.RegisterRollbackHook(nil)
	conn.RegisterAuthorizer(nil)
}

func (s *sqliteConnectionState) authorizer(action int, arg1, arg2, _ string) int {
	// Reaching authorizer for another prepared statement proves that any
	// earlier commit-hook boundary belongs to a completed statement. This is
	// the native boundary signal needed when a later statement in one Exec
	// fails before its own pre-update hook runs.
	if len(s.commitEnds) > s.confirmedEnds {
		s.confirmedEnds = len(s.commitEnds)
	}
	switch action {
	case sqlite3.SQLITE_CREATE_INDEX,
		sqlite3.SQLITE_CREATE_TABLE,
		sqlite3.SQLITE_CREATE_TEMP_INDEX,
		sqlite3.SQLITE_CREATE_TEMP_TABLE,
		sqlite3.SQLITE_CREATE_TEMP_TRIGGER,
		sqlite3.SQLITE_CREATE_TEMP_VIEW,
		sqlite3.SQLITE_CREATE_TRIGGER,
		sqlite3.SQLITE_CREATE_VIEW,
		sqlite3.SQLITE_DROP_INDEX,
		sqlite3.SQLITE_DROP_TABLE,
		sqlite3.SQLITE_DROP_TEMP_INDEX,
		sqlite3.SQLITE_DROP_TEMP_TABLE,
		sqlite3.SQLITE_DROP_TEMP_TRIGGER,
		sqlite3.SQLITE_DROP_TEMP_VIEW,
		sqlite3.SQLITE_DROP_TRIGGER,
		sqlite3.SQLITE_DROP_VIEW,
		sqlite3.SQLITE_ALTER_TABLE,
		sqlite3.SQLITE_ATTACH,
		sqlite3.SQLITE_DETACH:
		s.prepareMeta.ddl = true
	case sqlite3.SQLITE_CREATE_VTABLE:
		s.prepareMeta.ddl = true
		s.prepareMeta.unsupported = fmt.Errorf("sqlite mutation observer cannot observe virtual table %s", arg1)
	case sqlite3.SQLITE_DROP_VTABLE:
		s.prepareMeta.ddl = true
		s.prepareMeta.unsupported = fmt.Errorf("sqlite mutation observer cannot observe dropped virtual table %s", arg1)
	case sqlite3.SQLITE_SAVEPOINT:
		verb, name := authorizerSavepoint(arg1, arg2)
		if verb != "" {
			s.prepareMeta.savepointCount++
			s.prepareMeta.savepointVerb = verb
			s.prepareMeta.savepointName = name
		}
	}
	return sqlite3.SQLITE_OK
}

func authorizerSavepoint(operation, name string) (string, string) {
	switch strings.ToLower(operation) {
	case "begin":
		return "savepoint", normalizeSavepointName(name)
	case "rollback":
		return "rollback to", normalizeSavepointName(name)
	case "release":
		return "release", normalizeSavepointName(name)
	default:
		return "", ""
	}
}

func (s *sqliteConnectionState) preUpdate(data sqlite3.SQLitePreUpdateData) {
	if s.failed != nil || s.statementDDL || strings.HasPrefix(strings.ToLower(data.TableName), "sqlite_") {
		return
	}
	// A later statement can only reach pre-update after the preceding
	// autocommit callback returned successfully. Confirm that preceding fence
	// boundary before collecting the new candidate.
	if len(s.commitEnds) > s.confirmedEnds {
		s.confirmedEnds = len(s.commitEnds)
	}
	count := data.Count()
	var before, after []any
	var err error
	switch data.Op {
	case sqlite3.SQLITE_INSERT:
		after, err = scanSQLiteRow(&data, count, true)
	case sqlite3.SQLITE_UPDATE:
		before, err = scanSQLiteRow(&data, count, false)
		if err == nil {
			after, err = scanSQLiteRow(&data, count, true)
		}
	case sqlite3.SQLITE_DELETE:
		before, err = scanSQLiteRow(&data, count, false)
	}
	if err != nil {
		s.failed = err
		return
	}

	op := "unknown"
	switch data.Op {
	case sqlite3.SQLITE_INSERT:
		op = "insert"
	case sqlite3.SQLITE_UPDATE:
		op = "update"
	case sqlite3.SQLITE_DELETE:
		op = "delete"
	}
	s.pending = append(s.pending, sqlapi.Mutation{
		Schema:   data.DatabaseName,
		Table:    data.TableName,
		OldRowID: data.OldRowID,
		RowID:    data.NewRowID,
		Before:   before,
		After:    after,
		Op:       op,
	})
	s.pendingBytes = saturatingAdd(s.pendingBytes, mutationSize(s.pending[len(s.pending)-1]))
	if (s.maxChanges > 0 && len(s.pending) > s.maxChanges) || (s.maxBytes > 0 && s.pendingBytes > s.maxBytes) {
		s.failed = errObserverOverflow
		return
	}
	s.dmlInTxn = true
}

func (s *sqliteConnectionState) commit() int {
	// A single go-sqlite3 ExecContext may execute several semicolon-separated
	// statements inside one C call. Reuse the fence when SQLite invokes the
	// commit hook more than once before the wrapper gets control back; the
	// wrapper publishes the combined candidate and releases it exactly once.
	if !s.fenceHeld {
		s.backend.acquireCommitFence()
		s.fenceHeld = true
	}
	s.commitPending = true
	s.commitEnds = append(s.commitEnds, len(s.pending))
	return 0
}

func (s *sqliteConnectionState) rollback() {
	s.rollbackSeen = true
	// A failed ROLLBACK TO/RELEASE SAVEPOINT can invoke SQLite's rollback
	// hook even though the surrounding transaction remains active. The
	// authorizer metadata identifies that control statement; defer bookkeeping
	// until the wrapper sees its actual error instead of discarding the outer
	// transaction candidate here.
	if s.prepareMeta.savepointVerb != "" || s.statementSavepointVerb != "" {
		return
	}
	if len(s.commitEnds) > s.confirmedEnds {
		s.rollbackUnconfirmed = true
	}
	if len(s.commitEnds) > 0 {
		s.commitPending = true
		s.failed = nil
		return
	}
	s.pending = nil
	s.pendingBytes = 0
	s.savepoints = nil
	s.statementMark = 0
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	s.prepareMeta = statementMeta{}
	s.commitPending = false
	s.commitEnds = nil
	s.confirmedEnds = 0
	if s.fenceHeld {
		s.fenceHeld = false
		s.backend.releaseFence()
	}
	s.ddlInTxn = false
	s.dmlInTxn = false
	s.failed = nil
	s.statementDDL = false
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	s.prepareMeta = statementMeta{}
	s.unsupported = nil
}

func (s *sqliteConnectionState) statementBegin(_ string) {
	s.statementBeginWithMeta(statementMeta{})
}

func (s *sqliteConnectionState) statementBeginWithMeta(meta statementMeta) {
	s.statementMark = len(s.pending)
	s.statementDDL = meta.ddl
	s.statementSavepointVerb = meta.savepointVerb
	s.statementSavepointName = meta.savepointName
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	s.prepareMeta = statementMeta{}
}

func (s *sqliteConnectionState) statementEnd(_ string, err error) {
	s.statementEndWithMeta(err, statementMeta{})
}

func (s *sqliteConnectionState) statementEndWithMeta(err error, meta statementMeta) {
	s.applyStatementMeta(meta)
	if s.rollbackUnconfirmed {
		s.resolveRollbackOutcome()
	}
	if err == nil {
		s.confirmedEnds = len(s.commitEnds)
	} else if !s.rollbackSeen {
		// A commit hook boundary is authoritative once the driver call has
		// returned without a rollback callback. Keep confirmed prefixes even
		// when a later native statement in the same call reports an error.
		s.confirmedEnds = len(s.commitEnds)
	}
	lastBoundary := 0
	if len(s.commitEnds) > 0 {
		lastBoundary = s.commitEnds[len(s.commitEnds)-1]
	}
	residual := len(s.pending) > s.statementMark
	if len(s.commitEnds) > 0 {
		residual = len(s.pending) > lastBoundary
	}
	if err != nil && !s.rollbackSeen && residual {
		// SQLite's pre-update hook does not expose whether a failed DML
		// statement used ABORT or FAIL. Do not infer the conflict mode from
		// caller SQL; closing the observer is safer than publishing a partial
		// candidate whose commit status is unknown.
		s.failAmbiguous()
		truncate := s.statementMark
		if lastBoundary > truncate {
			truncate = lastBoundary
		}
		if truncate <= len(s.pending) {
			s.pending = s.pending[:truncate]
		}
		s.recomputePendingBytes()
	}
	s.statementMark = 0
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	if err == nil {
		if s.statementDDL {
			s.ddlInTxn = true
		}
		s.applySavepoint()
	}
	s.statementDDL = false
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	if s.unsupported != nil && s.backend.hasObservers() {
		s.backend.fail(s.unsupported)
	}
	s.unsupported = nil
	if s.failed != nil {
		if s.commitPending {
			s.finalize()
		}
		return
	}
	if s.commitPending {
		s.finalize()
	}
}

// resolveRollbackOutcome runs after SQLite has returned from the physical
// operation. A rollback hook can follow a committed autocommit prefix when a
// later native statement in the same Exec fails, but it can also follow a
// failed physical commit. The hook alone cannot distinguish those cases. The
// wrapper therefore verifies the unconfirmed net rows on the same connection;
// if they are not provably committed, the observer fails closed.
func (s *sqliteConnectionState) resolveRollbackOutcome() {
	if !s.rollbackUnconfirmed {
		return
	}
	if s.sqlite == nil || !s.sqlite.AutoCommit() {
		s.failAmbiguous()
		return
	}
	base := 0
	if s.confirmedEnds > 0 {
		base = s.commitEnds[s.confirmedEnds-1]
	}
	if base > len(s.pending) {
		s.failAmbiguous()
		return
	}
	changes, err := s.netChangesFor(s.pending[base:])
	if err != nil {
		s.failAmbiguous()
		return
	}
	committed, err := s.finalRowsMatch(changes)
	if err != nil {
		s.failAmbiguous()
		return
	}
	if !committed {
		if s.confirmedEnds > 0 {
			s.pending = s.pending[:base]
			s.recomputePendingBytes()
			s.commitEnds = s.commitEnds[:s.confirmedEnds]
			s.rollbackUnconfirmed = false
			return
		}
		s.failAmbiguous()
		return
	}
	s.confirmedEnds = len(s.commitEnds)
	s.rollbackUnconfirmed = false
}

func (s *sqliteConnectionState) finalRowsMatch(changes []sqlapi.Mutation) (bool, error) {
	if len(changes) == 0 {
		return true, nil
	}
	idsByTable := make(map[string][]int64)
	seen := make(map[string]map[int64]struct{})
	for _, change := range changes {
		rowID := change.RowID
		if change.Op == "delete" {
			rowID = change.OldRowID
		}
		if rowID == 0 {
			return false, fmt.Errorf("sqlite mutation observer cannot verify %s.%s row", change.Schema, change.Table)
		}
		tableKey := change.Schema + "\x00" + change.Table
		if seen[tableKey] == nil {
			seen[tableKey] = make(map[int64]struct{})
		}
		if _, ok := seen[tableKey][rowID]; !ok {
			seen[tableKey][rowID] = struct{}{}
			idsByTable[tableKey] = append(idsByTable[tableKey], rowID)
		}
	}

	rowsByKey := make(map[mutationKey][]any)
	for tableKey, rowIDs := range idsByTable {
		parts := strings.SplitN(tableKey, "\x00", 2)
		if len(parts) != 2 {
			return false, errors.New("sqlite mutation observer table key is invalid")
		}
		for start := 0; start < len(rowIDs); start += 500 {
			end := start + 500
			if end > len(rowIDs) {
				end = len(rowIDs)
			}
			placeholders := make([]string, end-start)
			args := make([]driver.Value, end-start)
			for i, rowID := range rowIDs[start:end] {
				placeholders[i] = "?"
				args[i] = rowID
			}
			query := fmt.Sprintf("SELECT rowid, * FROM %s.%s WHERE rowid IN (%s)", quoteIdentifier(parts[0]), quoteIdentifier(parts[1]), strings.Join(placeholders, ","))
			rawRows, err := s.sqlite.Query(query, args)
			if err != nil {
				return false, err
			}
			values := make([]driver.Value, len(rawRows.Columns()))
			for {
				err = rawRows.Next(values)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					_ = rawRows.Close()
					return false, err
				}
				rowID, ok := values[0].(int64)
				if !ok {
					_ = rawRows.Close()
					return false, fmt.Errorf("sqlite mutation observer returned rowid type %T", values[0])
				}
				after := make([]any, len(values)-1)
				for i := range after {
					after[i] = values[i+1]
				}
				rowsByKey[mutationKey{schema: parts[0], table: parts[1], rowID: rowID}] = after
			}
			_ = rawRows.Close()
		}
	}

	for _, change := range changes {
		rowID := change.RowID
		if change.Op == "delete" {
			rowID = change.OldRowID
		}
		row, exists := rowsByKey[mutationKey{schema: change.Schema, table: change.Table, rowID: rowID}]
		switch change.Op {
		case "delete":
			if exists {
				return false, nil
			}
		case "insert", "update":
			if !exists || !mutationValuesEqual(row, change.After) {
				return false, nil
			}
		default:
			return false, fmt.Errorf("sqlite mutation observer cannot verify operation %q", change.Op)
		}
	}
	return true, nil
}

func mutationValuesEqual(left, right []any) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !mutationValueEqual(left[i], right[i]) {
			return false
		}
	}
	return true
}

func mutationValueEqual(left, right any) bool {
	if leftBytes, ok := left.([]byte); ok {
		switch rightValue := right.(type) {
		case []byte:
			return bytes.Equal(leftBytes, rightValue)
		case string:
			return string(leftBytes) == rightValue
		}
	}
	if rightBytes, ok := right.([]byte); ok {
		if leftString, ok := left.(string); ok {
			return leftString == string(rightBytes)
		}
	}
	return reflect.DeepEqual(left, right)
}

func (s *sqliteConnectionState) applyStatementMeta(meta statementMeta) {
	if s.prepareMeta.savepointCount > 1 || meta.savepointCount > 1 {
		s.failAmbiguous()
	}
	if s.prepareMeta.ddl || meta.ddl {
		s.statementDDL = true
	}
	if s.prepareMeta.unsupported != nil {
		s.unsupported = s.prepareMeta.unsupported
	}
	if meta.unsupported != nil {
		s.unsupported = meta.unsupported
	}
	if s.prepareMeta.savepointVerb != "" {
		s.statementSavepointVerb = s.prepareMeta.savepointVerb
		s.statementSavepointName = s.prepareMeta.savepointName
	}
	if meta.savepointVerb != "" {
		s.statementSavepointVerb = meta.savepointVerb
		s.statementSavepointName = meta.savepointName
	}
	s.prepareMeta = statementMeta{}
}

func (s *sqliteConnectionState) failAmbiguous() {
	s.failed = errObserverAmbiguous
	if s.backend.hasObservers() {
		s.backend.fail(errObserverAmbiguous)
	}
}

func (s *sqliteConnectionState) finalizeAfterError(_ string, err error) {
	s.statementEndWithMeta(err, statementMeta{})
}

// finalize runs only after SQLite has returned from the operation that caused
// the commit hook. The hook is therefore a candidate marker; it never emits
// data itself. Net reduction uses the hook images collected on this physical
// connection, after statement/savepoint outcomes are known.
func (s *sqliteConnectionState) finalize() {
	if !s.commitPending {
		return
	}
	s.commitPending = false
	defer func() {
		if s.fenceHeld {
			s.fenceHeld = false
			s.backend.releaseFence()
		}
	}()
	if !s.backend.hasObservers() {
		s.resetTransaction()
		return
	}
	if s.failed != nil {
		s.backend.fail(s.failed)
		s.resetTransaction()
		return
	}
	if s.ddlInTxn && s.dmlInTxn {
		s.backend.fail(errors.New("sqlite mutation observer cannot represent DDL and DML in one transaction"))
		s.resetTransaction()
		return
	}
	ends := s.commitEnds
	if len(ends) == 0 {
		ends = []int{len(s.pending)}
	}
	start := 0
	for _, end := range ends {
		if end < start || end > len(s.pending) {
			s.backend.fail(errors.New("sqlite mutation observer commit boundary is invalid"))
			s.resetTransaction()
			return
		}
		changes, err := s.netChangesFor(s.pending[start:end])
		if err != nil {
			s.backend.fail(err)
			s.resetTransaction()
			return
		}
		s.backend.publish(changes)
		start = end
	}
	s.resetTransaction()
}

func (s *sqliteConnectionState) resetTransaction() {
	s.pending = nil
	s.pendingBytes = 0
	s.savepoints = nil
	s.statementMark = 0
	s.statementSavepointVerb = ""
	s.statementSavepointName = ""
	s.prepareMeta = statementMeta{}
	s.commitPending = false
	s.commitEnds = nil
	s.confirmedEnds = 0
	s.ddlInTxn = false
	s.dmlInTxn = false
	s.statementDDL = false
	s.unsupported = nil
	s.rollbackSeen = false
	s.rollbackUnconfirmed = false
	s.prepareMeta = statementMeta{}
}

// Savepoint state is applied only after SQLite reports success. A failed
// ROLLBACK TO/RELEASE must not change the candidate transaction in memory.
func (s *sqliteConnectionState) applySavepoint() {
	verb, name := s.statementSavepointVerb, s.statementSavepointName
	switch verb {
	case "savepoint":
		s.savepoints = append(s.savepoints, savepoint{name: name, index: len(s.pending)})
	case "rollback to":
		if index, ok := s.findSavepoint(name); ok {
			s.pending = s.pending[:index]
			s.recomputePendingBytes()
			for i := len(s.savepoints) - 1; i >= 0; i-- {
				if s.savepoints[i].name == name {
					s.savepoints = s.savepoints[:i+1]
					break
				}
			}
		}
	case "release":
		if index, ok := s.findSavepoint(name); ok {
			for i := len(s.savepoints) - 1; i >= 0; i-- {
				if s.savepoints[i].index == index {
					s.savepoints = s.savepoints[:i]
					break
				}
			}
		}
	}
}

func (s *sqliteConnectionState) recomputePendingBytes() {
	s.pendingBytes = 0
	for _, change := range s.pending {
		s.pendingBytes += mutationSize(change)
	}
}

type mutationKey struct {
	schema string
	table  string
	rowID  int64
}

type netMutation struct {
	mutation sqlapi.Mutation
	first    string
	last     string
}

// netChanges retains the earliest before-image for each row and the latest
// pre-update after-image. SQLite invokes the hook for every trigger-generated
// row change too, so the latest image is the committed row state without an
// O(N) SELECT round trip during the commit fence. Table metadata is resolved
// once per touched table.
func (s *sqliteConnectionState) netChangesFor(pending []sqlapi.Mutation) ([]sqlapi.Mutation, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	if s.sqlite == nil {
		return nil, errors.New("sqlite mutation observer has no physical connection")
	}
	nets := make(map[mutationKey]*netMutation, len(pending))
	order := make([]mutationKey, 0, len(pending))
	aliases := make(map[mutationKey]mutationKey)
	columnsByTable := make(map[string][]string)
	for _, change := range pending {
		if change.OldRowID == 0 && change.RowID == 0 {
			return nil, fmt.Errorf("sqlite mutation observer cannot identify %s.%s row", change.Schema, change.Table)
		}
		tableKey := change.Schema + "\x00" + change.Table
		columns, ok := columnsByTable[tableKey]
		if !ok {
			if err := s.validateTable(change.Schema, change.Table); err != nil {
				return nil, err
			}
			var err error
			columns, err = s.tableColumns(change.Schema, change.Table)
			if err != nil {
				return nil, err
			}
			columnsByTable[tableKey] = columns
		}
		key := mutationKey{schema: change.Schema, table: change.Table, rowID: change.OldRowID}
		if key.rowID == 0 {
			key.rowID = change.RowID
		}
		if alias, ok := aliases[key]; ok {
			key = alias
		}
		current, ok := nets[key]
		if !ok {
			current = &netMutation{mutation: change, first: change.Op, last: change.Op}
			current.mutation.Columns = columns
			nets[key] = current
			order = append(order, key)
		} else {
			current.last = change.Op
			current.mutation.Columns = columns
			if change.Op == "delete" {
				current.mutation.After = nil
			} else if change.After != nil {
				current.mutation.After = change.After
			}
			if current.mutation.RowID != change.RowID && change.RowID != 0 {
				aliases[mutationKey{schema: change.Schema, table: change.Table, rowID: change.RowID}] = key
				current.mutation.RowID = change.RowID
			}
			if current.mutation.OldRowID == 0 {
				current.mutation.OldRowID = change.OldRowID
			}
		}
		if change.RowID != 0 {
			aliases[mutationKey{schema: change.Schema, table: change.Table, rowID: change.RowID}] = key
		}
	}

	result := make([]sqlapi.Mutation, 0, len(order))
	for _, key := range order {
		current := nets[key]
		if current.first == "insert" && current.last == "delete" {
			continue
		}
		change := current.mutation
		switch {
		case current.last == "delete":
			change.Op = "delete"
			change.RowID = 0
		case current.first == "insert":
			change.Op = "insert"
		default:
			change.Op = "update"
		}
		result = append(result, change)
	}
	return result, nil
}

func (s *sqliteConnectionState) findSavepoint(name string) (int, bool) {
	for i := len(s.savepoints) - 1; i >= 0; i-- {
		if s.savepoints[i].name == name {
			return s.savepoints[i].index, true
		}
	}
	return 0, false
}

func (s *sqliteConnectionState) validateTable(schema, table string) error {
	if strings.EqualFold(schema, "temp") {
		return errors.New("sqlite mutation observer does not support TEMP tables")
	}
	query := fmt.Sprintf("SELECT sql FROM %s.sqlite_master WHERE type = 'table' AND name = ?", quoteIdentifier(schema))
	rows, err := s.sqlite.Query(query, []driver.Value{table})
	if err != nil {
		return fmt.Errorf("inspect sqlite table %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	values := make([]driver.Value, len(rows.Columns()))
	if err := rows.Next(values); err != nil {
		if err == io.EOF {
			return fmt.Errorf("sqlite table %s.%s disappeared", schema, table)
		}
		return err
	}
	definition := ""
	switch value := values[0].(type) {
	case string:
		definition = value
	case []byte:
		definition = string(value)
	}
	upper := strings.ToUpper(definition)
	if strings.Contains(upper, "WITHOUT ROWID") {
		return fmt.Errorf("sqlite mutation observer does not support WITHOUT ROWID table %s.%s", schema, table)
	}
	if strings.HasPrefix(strings.TrimSpace(upper), "CREATE VIRTUAL TABLE") {
		return fmt.Errorf("sqlite mutation observer does not support virtual table %s.%s", schema, table)
	}
	return nil
}

func (s *sqliteConnectionState) tableColumns(schema, table string) ([]string, error) {
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT 0", quoteIdentifier(schema), quoteIdentifier(table))
	rows, err := s.sqlite.Query(query, nil)
	if err != nil {
		return nil, fmt.Errorf("read sqlite columns %s.%s: %w", schema, table, err)
	}
	defer rows.Close()
	return append([]string(nil), rows.Columns()...), nil
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
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
	name = strings.TrimSpace(strings.TrimSuffix(name, ";"))
	name = strings.Trim(name, "`\"[]")
	return strings.ToLower(name)
}

// observedConn delegates all normal SQL behavior while ensuring every physical
// connection operation gets a post-operation finalization point.
type observedConn struct {
	raw     driver.Conn
	sqlite  *sqlite3.SQLiteConn
	backend *sqliteBackend
	state   *sqliteConnectionState
}

func (c *observedConn) bindIfActive() {
	c.state.bind(c.sqlite)
}

func (c *observedConn) clearHooks() { c.state.clear(c.sqlite) }

func (c *observedConn) Prepare(query string) (driver.Stmt, error) {
	c.state.prepareMeta = statementMeta{}
	stmt, err := c.raw.Prepare(query)
	meta := c.state.prepareMeta
	c.state.prepareMeta = statementMeta{}
	if err != nil {
		return nil, err
	}
	return &observedStmt{raw: stmt, conn: c, query: query, meta: meta}, nil
}

func (c *observedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	preparer, ok := c.raw.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.prepareMeta = statementMeta{}
	stmt, err := preparer.PrepareContext(ctx, query)
	meta := c.state.prepareMeta
	c.state.prepareMeta = statementMeta{}
	if err != nil {
		return nil, err
	}
	return &observedStmt{raw: stmt, conn: c, query: query, meta: meta}, nil
}

func (c *observedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := c.raw.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.statementBegin(query)
	result, err := execer.ExecContext(ctx, query, args)
	c.state.statementEnd(query, err)
	return result, err
}

func (c *observedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := c.raw.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	c.state.statementBegin(query)
	rows, err := queryer.QueryContext(ctx, query, args)
	if err != nil {
		c.state.statementEnd(query, err)
		return nil, err
	}
	return &observedRows{raw: rows, conn: c, query: query}, nil
}

func (c *observedConn) CheckNamedValue(value *driver.NamedValue) error {
	checker, ok := c.raw.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}
	return checker.CheckNamedValue(value)
}

func (c *observedConn) Ping(ctx context.Context) error {
	pinger, ok := c.raw.(driver.Pinger)
	if !ok {
		return nil
	}
	return pinger.Ping(ctx)
}

func (c *observedConn) Close() error {
	c.state.rollback()
	return c.raw.Close()
}

func (c *observedConn) Begin() (driver.Tx, error) {
	tx, err := c.raw.Begin()
	if err != nil {
		return nil, err
	}
	return &observedTx{raw: tx, conn: c}, nil
}

func (c *observedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	begin, ok := c.raw.(driver.ConnBeginTx)
	if !ok {
		return c.Begin()
	}
	tx, err := begin.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &observedTx{raw: tx, conn: c}, nil
}

type observedTx struct {
	raw  driver.Tx
	conn *observedConn
}

func (t *observedTx) Commit() error {
	err := t.raw.Commit()
	if t.conn.state.commitPending {
		t.conn.state.finalizeAfterError("COMMIT", err)
	}
	return err
}

func (t *observedTx) Rollback() error {
	err := t.raw.Rollback()
	t.conn.state.rollback()
	return err
}

type observedStmt struct {
	raw   driver.Stmt
	conn  *observedConn
	query string
	meta  statementMeta
}

func (s *observedStmt) Close() error  { return s.raw.Close() }
func (s *observedStmt) NumInput() int { return s.raw.NumInput() }

func (s *observedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.raw.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.conn.state.statementBeginWithMeta(s.meta)
	result, err := execer.ExecContext(ctx, args)
	s.conn.state.statementEndWithMeta(err, s.meta)
	return result, err
}

func (s *observedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.raw.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	s.conn.state.statementBeginWithMeta(s.meta)
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		s.conn.state.statementEndWithMeta(err, s.meta)
		return nil, err
	}
	return &observedRows{raw: rows, conn: s.conn, query: s.query, meta: s.meta}, nil
}

func (s *observedStmt) ColumnConverter(index int) driver.ValueConverter {
	if converter, ok := s.raw.(driver.ColumnConverter); ok {
		return converter.ColumnConverter(index)
	}
	return driver.DefaultParameterConverter
}

func (s *observedStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.conn.state.statementBeginWithMeta(s.meta)
	result, err := s.raw.Exec(args)
	s.conn.state.statementEndWithMeta(err, s.meta)
	return result, err
}

func (s *observedStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.state.statementBeginWithMeta(s.meta)
	rows, err := s.raw.Query(args)
	if err != nil {
		s.conn.state.statementEndWithMeta(err, s.meta)
		return nil, err
	}
	return &observedRows{raw: rows, conn: s.conn, query: s.query, meta: s.meta}, nil
}

type observedRows struct {
	raw    driver.Rows
	conn   *observedConn
	query  string
	meta   statementMeta
	closed bool
	mu     sync.Mutex
}

func (r *observedRows) Columns() []string { return r.raw.Columns() }

func (r *observedRows) Next(dest []driver.Value) error {
	err := r.raw.Next(dest)
	if err == io.EOF || err != nil {
		r.finish(err)
	}
	return err
}

func (r *observedRows) Close() error {
	err := r.raw.Close()
	r.finish(err)
	return err
}

func (r *observedRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.raw.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *observedRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *observedRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *observedRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.raw.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (r *observedRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.raw.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}

func (r *observedRows) finish(err error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()
	r.conn.state.statementEndWithMeta(err, r.meta)
}

type mutationStream struct {
	backend       *sqliteBackend
	opts          sqlapi.MutationOptions
	changes       chan sqlapi.MutationBatch
	notify        chan struct{}
	done          chan struct{}
	mu            sync.Mutex
	err           error
	closed        bool
	snapshotting  bool
	watermark     string
	queue         []sqlapi.MutationBatch
	pending       []sqlapi.MutationBatch
	queuedChanges int
	queuedBytes   int
	cancel        context.CancelFunc
	maxChanges    int
	maxBytes      int
}

func newMutationStream(backend *sqliteBackend, opts sqlapi.MutationOptions) *mutationStream {
	stream := &mutationStream{
		backend:    backend,
		opts:       opts,
		changes:    make(chan sqlapi.MutationBatch),
		notify:     make(chan struct{}, 1),
		done:       make(chan struct{}),
		maxChanges: opts.MaxChanges,
		maxBytes:   opts.MaxBytes,
	}
	go stream.relay()
	return stream
}

func newSnapshotStream(backend *sqliteBackend, opts sqlapi.SnapshotOptions, watermark string, cancel context.CancelFunc) *mutationStream {
	stream := newMutationStream(backend, sqlapi.MutationOptions{
		Tables: opts.Tables, MaxChanges: opts.MaxChanges, MaxBytes: opts.MaxBytes,
	})
	stream.snapshotting = true
	stream.watermark = watermark
	stream.cancel = cancel
	return stream
}

func (s *mutationStream) Changes() <-chan sqlapi.MutationBatch { return s.changes }

func (s *mutationStream) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	return err
}

func (s *mutationStream) Close() error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.backend.remove(s, nil)
	return nil
}

func (s *mutationStream) push(batch sqlapi.MutationBatch) {
	batch = filterBatch(batch, s.opts)
	if len(batch.Changes) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.snapshotting {
		if !s.enqueuePendingLocked(batch) {
			s.closeLocked(errObserverOverflow)
			return
		}
		return
	}
	if !s.enqueueLocked(batch) {
		s.closeLocked(errObserverOverflow)
	}
}

func filterBatch(batch sqlapi.MutationBatch, opts sqlapi.MutationOptions) sqlapi.MutationBatch {
	if len(opts.Tables) == 0 && len(opts.Operations) == 0 {
		return batch
	}
	filtered := make([]sqlapi.Mutation, 0, len(batch.Changes))
	for _, change := range batch.Changes {
		if len(opts.Tables) > 0 && !matchesTable(change.Schema, change.Table, opts.Tables) {
			continue
		}
		if len(opts.Operations) > 0 && !matchesValue(change.Op, opts.Operations) {
			continue
		}
		filtered = append(filtered, change)
	}
	batch.Changes = filtered
	return batch
}

func matchesTable(schema, table string, filters []string) bool {
	for _, filter := range filters {
		if filter == table || filter == schema+"."+table {
			return true
		}
	}
	return false
}

func matchesValue(value string, filters []string) bool {
	for _, filter := range filters {
		if strings.EqualFold(value, filter) {
			return true
		}
	}
	return false
}

func (s *mutationStream) pushSnapshot(batch sqlapi.MutationBatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if s.err != nil {
			return s.err
		}
		return errObserverClosed
	}
	if !s.enqueueLocked(batch) {
		s.closeLocked(errObserverOverflow)
		return errObserverOverflow
	}
	return nil
}

func (s *mutationStream) finishSnapshot(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if err != nil {
		s.closeLocked(err)
		return
	}
	s.snapshotting = false
	s.queue = append(s.queue, s.pending...)
	s.pending = nil
	s.signalLocked()
}

func (s *mutationStream) Watermark() string { return s.watermark }

func (s *mutationStream) enqueueLocked(batch sqlapi.MutationBatch) bool {
	changes := len(batch.Changes)
	bytes := mutationBatchBytes(batch)
	if s.maxChanges > 0 && (changes > s.maxChanges || s.queuedChanges > s.maxChanges-changes) {
		return false
	}
	if s.maxBytes > 0 && (bytes > s.maxBytes || s.queuedBytes > s.maxBytes-bytes) {
		return false
	}
	s.queue = append(s.queue, batch)
	s.queuedChanges = saturatingAdd(s.queuedChanges, changes)
	s.queuedBytes = saturatingAdd(s.queuedBytes, bytes)
	s.signalLocked()
	return true
}

func (s *mutationStream) enqueuePendingLocked(batch sqlapi.MutationBatch) bool {
	changes := len(batch.Changes)
	bytes := mutationBatchBytes(batch)
	if s.maxChanges > 0 && (changes > s.maxChanges || s.queuedChanges > s.maxChanges-changes) {
		return false
	}
	if s.maxBytes > 0 && (bytes > s.maxBytes || s.queuedBytes > s.maxBytes-bytes) {
		return false
	}
	s.pending = append(s.pending, batch)
	s.queuedChanges = saturatingAdd(s.queuedChanges, changes)
	s.queuedBytes = saturatingAdd(s.queuedBytes, bytes)
	s.signalLocked()
	return true
}

func (s *mutationStream) signalLocked() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *mutationStream) relay() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			closed := s.closed
			s.mu.Unlock()
			if closed {
				close(s.changes)
				return
			}
			<-s.notify
			continue
		}
		batch := s.queue[0]
		s.mu.Unlock()

		select {
		case s.changes <- batch:
		case <-s.done:
			close(s.changes)
			return
		}

		s.mu.Lock()
		if len(s.queue) > 0 {
			s.queue = s.queue[1:]
			s.queuedChanges -= len(batch.Changes)
			s.queuedBytes -= mutationBatchBytes(batch)
		}
		s.mu.Unlock()
	}
}

func mutationBatchBytes(batch sqlapi.MutationBatch) int {
	bytes := saturatingAdd(mutationStructuralBytes, len(batch.Transaction))
	for _, change := range batch.Changes {
		bytes = saturatingAdd(bytes, mutationSize(change))
	}
	return bytes
}

func mutationSize(change sqlapi.Mutation) int {
	bytes := mutationStructuralBytes
	bytes = saturatingAdd(bytes, len(change.Schema))
	bytes = saturatingAdd(bytes, len(change.Table))
	bytes = saturatingAdd(bytes, len(change.Op))
	for _, column := range change.Columns {
		bytes = saturatingAdd(bytes, len(column))
	}
	bytes = saturatingAdd(bytes, mutationValuesBytes(change.Before))
	return saturatingAdd(bytes, mutationValuesBytes(change.After))
}

func mutationValuesBytes(values []any) int {
	bytes := 0
	for _, value := range values {
		bytes = saturatingAdd(bytes, valueStructuralBytes)
		switch value := value.(type) {
		case nil:
			bytes = saturatingAdd(bytes, 1)
		case []byte:
			bytes = saturatingAdd(bytes, len(value))
		case string:
			bytes = saturatingAdd(bytes, len(value))
		default:
			bytes = saturatingAdd(bytes, 16)
		}
	}
	return bytes
}

func saturatingAdd(left, right int) int {
	if left < 0 || right < 0 {
		return int(^uint(0) >> 1)
	}
	maxInt := int(^uint(0) >> 1)
	if left > maxInt-right {
		return maxInt
	}
	return left + right
}

func (s *mutationStream) closeWithError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(err)
}

func (s *mutationStream) closeLocked(err error) {
	if s.closed {
		return
	}
	s.closed = true
	s.err = err
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	close(s.done)
	s.queue = nil
	s.pending = nil
	s.queuedChanges = 0
	s.queuedBytes = 0
	s.signalLocked()
}

var _ driver.Connector = (*sqliteConnector)(nil)
var _ driver.Conn = (*observedConn)(nil)
var _ driver.Tx = (*observedTx)(nil)
var _ driver.Stmt = (*observedStmt)(nil)
var _ driver.Rows = (*observedRows)(nil)
var _ sqlapi.CommittedMutationSource = (*sqliteBackend)(nil)
var _ sqlapi.MutationStream = (*mutationStream)(nil)
