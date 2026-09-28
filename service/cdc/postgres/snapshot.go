// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	config "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

func (s *Source) startSnapshot(ctx context.Context, sub *sourceSubscription) {
	snapshotCtx, cancel := context.WithCancel(ctx)
	s.snapshotWG.Add(1)
	snapshotDone := make(chan struct{})
	if !sub.registerSnapshot(cancel, snapshotDone) {
		s.snapshotWG.Done()
		cancel()
		return
	}
	go func() {
		defer s.snapshotWG.Done()
		defer sub.finishSnapshotWorker()
		watchDone := make(chan struct{})
		go func() {
			select {
			case <-sub.done:
				cancel()
			case <-snapshotCtx.Done():
			case <-watchDone:
			}
		}()
		defer close(watchDone)

		if err := s.acquireSnapshot(snapshotCtx); err != nil {
			sub.finishSnapshot(0, err)
			return
		}
		defer s.releaseSnapshot()

		fence, err := s.snapshotCurrentTo(snapshotCtx, sub)
		if err != nil {
			sub.finishSnapshot(0, err)
			return
		}
		sub.finishSnapshot(fence, nil)
	}()
}

func (s *Source) acquireSnapshot(ctx context.Context) error {
	select {
	case s.snapshotGate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Source) releaseSnapshot() {
	<-s.snapshotGate
}

func (s *Source) waitSnapshots(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.snapshotWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type tableRef struct {
	schema string
	name   string
}

func (t tableRef) quoted() string {
	return pq.QuoteIdentifier(t.schema) + "." + pq.QuoteIdentifier(t.name)
}

func (s *Source) snapshotWithSink(ctx context.Context, adminDB *sql.DB, publication, snapshotName string, sink snapshotSink) error {
	conn, err := adminDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("snapshot connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
		return fmt.Errorf("begin snapshot transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")
		}
	}()

	setup := []string{
		"SET TRANSACTION SNAPSHOT " + pq.QuoteLiteral(snapshotName),
		"SET LOCAL bytea_output = 'hex'",
		"SET LOCAL extra_float_digits = 3",
		"SET LOCAL TimeZone = 'UTC'",
	}
	for _, stmt := range setup {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("snapshot session setup: %w", err)
		}
	}

	if snapshotFailpoint != nil {
		if err := snapshotFailpoint(); err != nil {
			return err
		}
	}

	tables, err := publishedTables(ctx, conn, publication)
	if err != nil {
		return err
	}

	total := 0
	for _, tbl := range tables {
		n, err := s.snapshotTableWithSink(ctx, conn, tbl, sink)
		if err != nil {
			return err
		}
		total += n
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit snapshot transaction: %w", err)
	}
	committed = true
	s.log.Info("cdc snapshot complete",
		zap.String("slot", s.slot), zap.Int("tables", len(tables)), zap.Int("rows", total))
	return nil
}

// snapshotCurrent establishes an exported logical-decoding snapshot for one
// subscriber. The temporary slot's consistent point is the exact WAL fence;
// unlike a SQL-only pg_current_wal_lsn query, it cannot race a concurrent
// commit between snapshot acquisition and fence capture.
func (s *Source) snapshotCurrentTo(ctx context.Context, sub *sourceSubscription) (pglogrepl.LSN, error) {
	replConn, err := pgconn.Connect(ctx, s.replDSN)
	if err != nil {
		return 0, fmt.Errorf("snapshot replication connection: %w", err)
	}
	defer func() { _ = replConn.Close(context.Background()) }()
	if _, err := pglogrepl.IdentifySystem(ctx, replConn); err != nil {
		return 0, fmt.Errorf("identify snapshot replication system: %w", err)
	}

	snapshotSlot := subscriberSnapshotSlot(s.slot, sub.id)
	snapshotSlotID, err := quoteReplicationSlotName(snapshotSlot)
	if err != nil {
		return 0, err
	}
	result, err := pglogrepl.CreateReplicationSlot(ctx, replConn, snapshotSlotID, config.OutputPlugin,
		pglogrepl.CreateReplicationSlotOptions{Temporary: true, SnapshotAction: "EXPORT_SNAPSHOT"})
	if err != nil {
		return 0, fmt.Errorf("create subscriber snapshot slot: %w", err)
	}
	fence, err := pglogrepl.ParseLSN(result.ConsistentPoint)
	if err != nil {
		return 0, fmt.Errorf("parse subscriber snapshot fence %q: %w", result.ConsistentPoint, err)
	}
	if result.SnapshotName == "" {
		return 0, errors.New("subscriber snapshot slot returned no exported snapshot")
	}
	if err := s.waitStreamPosition(ctx, fence); err != nil {
		return 0, fmt.Errorf("wait for replication fence: %w", err)
	}

	adminDB, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		return 0, fmt.Errorf("open snapshot connection: %w", err)
	}
	defer func() { _ = adminDB.Close() }()
	adminDB.SetMaxOpenConns(1)
	adminDB.SetMaxIdleConns(1)
	if err := adminDB.PingContext(ctx); err != nil {
		return 0, fmt.Errorf("ping snapshot connection: %w", err)
	}

	err = s.snapshotWithSink(ctx, adminDB, s.publication, result.SnapshotName, func(rc RowChange) error {
		if !sub.matchesSnapshot(config.Change{Table: rc.Table, Relation: rc.Relation()}) {
			return nil
		}
		change := config.Change{
			Source:    s.name,
			Op:        string(OpSnapshot),
			Schema:    rc.Schema,
			Table:     rc.Table,
			Relation:  rc.Relation(),
			CommitLSN: fence.String(),
			Before:    rc.Before,
			After:     rc.After,
		}
		return sub.sendSnapshot(change, config.EstimateChangeBytes(change))
	})
	if err != nil {
		return 0, fmt.Errorf("subscriber snapshot scan: %w", err)
	}
	return fence, nil
}

func subscriberSnapshotSlot(slot string, id uint64) string {
	digest := sha256.Sum256([]byte(slot))
	return fmt.Sprintf("wippy_snap_%x_%d", digest[:8], id)
}

type snapshotSink func(RowChange) error

func (s *Source) snapshotTableWithSink(ctx context.Context, conn *sql.Conn, tbl tableRef, sink snapshotSink) (int, error) {
	if _, err := conn.ExecContext(ctx,
		"DECLARE "+snapshotCursor+" NO SCROLL CURSOR FOR SELECT * FROM "+tbl.quoted()); err != nil {
		return 0, fmt.Errorf("declare cursor %s.%s: %w", tbl.schema, tbl.name, err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), snapshotCloseSQL) }()

	fetchSQL := fmt.Sprintf("FETCH %d FROM %s", s.snapshotFetchSize, snapshotCursor)
	n := 0
	for {
		got, err := s.fetchSnapshotBatch(ctx, conn, tbl, fetchSQL, sink)
		if err != nil {
			return n, err
		}
		n += got
		if got < s.snapshotFetchSize {
			return n, nil
		}
	}
}

func (s *Source) fetchSnapshotBatch(ctx context.Context, conn *sql.Conn, tbl tableRef, fetchSQL string, sink snapshotSink) (int, error) {
	rows, err := conn.QueryContext(ctx, fetchSQL)
	if err != nil {
		return 0, fmt.Errorf("fetch %s.%s: %w", tbl.schema, tbl.name, err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("snapshot columns %s.%s: %w", tbl.schema, tbl.name, err)
	}

	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	got := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return got, fmt.Errorf("scan snapshot row: %w", err)
		}
		after := make(map[string]any, len(cols))
		for i, c := range cols {
			if vals[i].Valid {
				after[c] = vals[i].String
			} else {
				after[c] = nil
			}
		}
		if err := sink(RowChange{Op: OpSnapshot, Schema: tbl.schema, Table: tbl.name, After: after}); err != nil {
			return got, err
		}
		got++
	}
	return got, rows.Err()
}

func publishedTables(ctx context.Context, conn *sql.Conn, publication string) ([]tableRef, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT schemaname, tablename,
		        rowfilter IS NOT NULL OR cardinality(attnames) <> (
		          SELECT count(*) FROM pg_attribute
		          WHERE attrelid = format('%I.%I', schemaname, tablename)::regclass
		            AND attnum > 0 AND NOT attisdropped)
		 FROM pg_publication_tables WHERE pubname = $1
		 ORDER BY schemaname, tablename`, publication)
	if err != nil {
		return nil, fmt.Errorf("list published tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []tableRef
	for rows.Next() {
		var t tableRef
		var restricted bool
		if err := rows.Scan(&t.schema, &t.name, &restricted); err != nil {
			return nil, fmt.Errorf("scan published table: %w", err)
		}
		if restricted {
			return nil, fmt.Errorf("%w: snapshot of publication %q has row filters or column lists on %s", config.ErrUnsupported, publication, t.quoted())
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}
