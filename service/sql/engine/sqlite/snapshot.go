// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

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
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		_ = tx.Rollback()
		_ = conn.Close()
		return nil, errObserverClosed
	}
	stream := newSnapshotStream(scanCtx, b, opts, watermark, cancel)
	b.streams[stream] = struct{}{}
	b.mu.Unlock()
	// The fence remains held until the stream is registered and its read view
	// has been established. New commits therefore receive a sequence greater
	// than watermark and are buffered by this stream.
	release = false
	b.releaseFence()
	stream.start()
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
		query := sqliteMasterQuery(table.schema)
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
	qualified := quoteIdentifier(schema) + "." + quoteIdentifier(table)
	metadata, err := tx.QueryContext(ctx, "SELECT * FROM "+qualified+" LIMIT 0")
	if err != nil {
		return fmt.Errorf("scan sqlite snapshot %s.%s: %w", schema, table, err)
	}
	columns, err := metadata.Columns()
	closeErr := metadata.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if len(columns) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+storageProjection(columns)+" FROM "+qualified)
	if err != nil {
		return err
	}
	defer rows.Close()
	batcher := newSnapshotBatcher(stream.watermark, batchSize, stream.maxChanges, stream.maxBytes)
	for rows.Next() {
		values := make([]any, len(columns))
		dest := make([]any, len(values))
		for i := range values {
			dest[i] = &values[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		change := sqlapi.Mutation{
			Schema: schema, Table: table, Columns: columns,
			After: values, Op: "snapshot",
		}
		if err := batcher.add(change, stream.pushSnapshot); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return batcher.flush(stream.pushSnapshot)
}

type snapshotBatcher struct {
	transaction string
	changes     []sqlapi.Mutation
	batchBytes  int
	batchSize   int
	maxChanges  int
	maxBytes    int
}

func newSnapshotBatcher(transaction string, batchSize, maxChanges, maxBytes int) *snapshotBatcher {
	return &snapshotBatcher{
		transaction: transaction,
		batchBytes:  mutationBatchBytes(sqlapi.MutationBatch{Transaction: transaction}),
		batchSize:   batchSize,
		maxChanges:  maxChanges,
		maxBytes:    maxBytes,
	}
}

func (b *snapshotBatcher) add(change sqlapi.Mutation, emit func(sqlapi.MutationBatch) error) error {
	changeBytes := mutationSize(change)
	if len(b.changes) > 0 {
		bytesExceed := b.maxBytes > 0 && (b.batchBytes > b.maxBytes || changeBytes > b.maxBytes-b.batchBytes)
		changesExceed := b.maxChanges > 0 && len(b.changes) >= b.maxChanges
		if bytesExceed || changesExceed {
			if err := b.flush(emit); err != nil {
				return err
			}
		}
	}
	b.changes = append(b.changes, change)
	b.batchBytes = saturatingAdd(b.batchBytes, changeBytes)
	if len(b.changes) >= b.batchSize ||
		(b.maxBytes > 0 && b.batchBytes >= b.maxBytes) ||
		(b.maxChanges > 0 && len(b.changes) >= b.maxChanges) {
		return b.flush(emit)
	}
	return nil
}

func (b *snapshotBatcher) flush(emit func(sqlapi.MutationBatch) error) error {
	if len(b.changes) == 0 {
		return nil
	}
	batch := sqlapi.MutationBatch{
		Transaction: b.transaction,
		Snapshot:    true,
		Changes:     append([]sqlapi.Mutation(nil), b.changes...),
	}
	b.changes = b.changes[:0]
	b.batchBytes = mutationBatchBytes(sqlapi.MutationBatch{Transaction: b.transaction})
	return emit(batch)
}
