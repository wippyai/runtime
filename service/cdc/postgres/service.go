// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/lib/pq"
	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	config "github.com/wippyai/runtime/api/service/cdc"
)

const retainedWALGauge = "wippy_cdc_retained_wal_bytes"

const (
	defaultStandbyInterval = 10 * time.Second
	defaultStatusInterval  = 30 * time.Second
	slotActiveSQLState     = "55006"
	slotDropMaxAttempts    = 10
	slotDropRetryDelay     = 100 * time.Millisecond
	snapshotFetchSize      = 1000
	snapshotCursor         = "wippy_cdc_snapshot"
	snapshotFetchSQL       = "FETCH 1000 FROM " + snapshotCursor
	snapshotCloseSQL       = "CLOSE " + snapshotCursor
)

type SourceOptions struct {
	Bus             event.Bus
	Checkpoint      Checkpointer
	Log             *zap.Logger
	ReplDSN         string
	AdminDSN        string
	Slot            string
	Publication     string
	EventSystem     string
	Tables          []string
	StandbyInterval time.Duration
	StatusInterval  time.Duration
	Temporary       bool
	Snapshot        bool
}

type Source struct {
	bus         event.Bus
	log         *zap.Logger
	injectedCP  Checkpointer
	cancel      context.CancelFunc
	done        chan struct{}
	replDSN     string
	adminDSN    string
	slot        string
	publication string
	eventSystem string
	tables      []string

	standbyInterval time.Duration
	statusInterval  time.Duration
	mu              sync.Mutex
	temporary       bool
	snapshot        bool
	stopped         atomic.Bool
	dropSlot        atomic.Bool
}

var snapshotFailpoint func() error

func (s *Source) MarkForSlotDrop() {
	s.dropSlot.Store(true)
}

func NewSource(opts SourceOptions) *Source {
	log := opts.Log
	if log == nil {
		log = zap.NewNop()
	}
	system := opts.EventSystem
	if system == "" {
		system = config.DefaultEventSystem
	}
	standby := opts.StandbyInterval
	if standby <= 0 {
		standby = defaultStandbyInterval
	}
	status := opts.StatusInterval
	if status <= 0 {
		status = defaultStatusInterval
	}
	return &Source{
		bus:             opts.Bus,
		log:             log,
		injectedCP:      opts.Checkpoint,
		replDSN:         opts.ReplDSN,
		adminDSN:        opts.AdminDSN,
		slot:            opts.Slot,
		publication:     opts.Publication,
		eventSystem:     system,
		tables:          opts.Tables,
		temporary:       opts.Temporary,
		snapshot:        opts.Snapshot,
		standbyInterval: standby,
		statusInterval:  status,
	}
}

func (s *Source) Start(ctx context.Context) (<-chan any, error) {
	if s.stopped.Load() {
		return nil, ErrSourceClosed
	}

	adminDB, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		return nil, fmt.Errorf("open admin connection: %w", err)
	}
	if err := adminDB.PingContext(ctx); err != nil {
		_ = adminDB.Close()
		return nil, fmt.Errorf("ping admin connection: %w", err)
	}

	cp := s.injectedCP
	if cp == nil {
		dbcp, cpErr := NewDBCheckpointer(ctx, adminDB)
		if cpErr != nil {
			_ = adminDB.Close()
			return nil, cpErr
		}
		cp = dbcp
	}

	publication, err := s.ensurePublication(ctx, adminDB)
	if err != nil {
		_ = adminDB.Close()
		return nil, err
	}

	conn, err := pgconn.Connect(ctx, s.replDSN)
	if err != nil {
		_ = adminDB.Close()
		return nil, fmt.Errorf("replication connect: %w", err)
	}

	sysident, err := pglogrepl.IdentifySystem(ctx, conn)
	if err != nil {
		_ = conn.Close(ctx)
		_ = adminDB.Close()
		return nil, fmt.Errorf("identify system: %w", err)
	}

	startLSN, snapshotName, err := s.prepareSlot(ctx, conn, adminDB, cp, sysident.XLogPos)
	if err != nil {
		_ = conn.Close(ctx)
		_ = adminDB.Close()
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	status := make(chan any, 8)
	done := make(chan struct{})

	s.mu.Lock()
	s.cancel = cancel
	s.done = done
	s.mu.Unlock()

	s.log.Info("cdc source started",
		zap.String("slot", s.slot),
		zap.String("publication", publication),
		zap.String("start_lsn", startLSN.String()),
		zap.Bool("snapshot", snapshotName != ""))
	select {
	case status <- "cdc replication started":
	default:
	}

	go s.run(runCtx, conn, adminDB, cp, startLSN, snapshotName, publication, metrics.GetCollector(ctx), status, done)
	return status, nil
}

func (s *Source) Stop(ctx context.Context) error {
	if !s.stopped.CompareAndSwap(false, true) {
		return nil
	}

	s.mu.Lock()
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if s.dropSlot.Load() && !s.temporary {
		return s.dropSlotAndCheckpoint(ctx)
	}
	return nil
}

func (s *Source) run(
	ctx context.Context,
	conn *pgconn.PgConn,
	adminDB *sql.DB,
	cp Checkpointer,
	startLSN pglogrepl.LSN,
	snapshotName string,
	publication string,
	mc metrics.Collector,
	status chan any,
	done chan struct{},
) {
	defer close(done)
	defer close(status)
	defer func() { _ = adminDB.Close() }()
	defer func() { _ = conn.Close(context.Background()) }()

	if snapshotName != "" {
		if err := s.snapshotExisting(ctx, adminDB, publication, snapshotName); err != nil {
			s.abortFreshSnapshot(conn)
			s.fail(ctx, status, err)
			return
		}
	}

	pluginArgs := []string{
		fmt.Sprintf("proto_version '%d'", config.ProtocolVersion),
		fmt.Sprintf("publication_names '%s'", publication),
	}
	if err := pglogrepl.StartReplication(ctx, conn, s.slot, startLSN,
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		s.fail(ctx, status, err)
		return
	}

	dec := newDecoder()
	clientPos := startLSN
	now := time.Now()
	nextStandby := now.Add(s.standbyInterval)
	nextStatus := now.Add(s.statusInterval)

	for {
		if ctx.Err() != nil {
			return
		}

		now = time.Now()
		if !now.Before(nextStandby) {
			if err := checkpointAndAck(ctx, conn, cp, s.slot, clientPos); err != nil {
				s.fail(ctx, status, err)
				return
			}
			nextStandby = now.Add(s.standbyInterval)
		}
		if !now.Before(nextStatus) {
			s.reportLag(ctx, adminDB, mc)
			nextStatus = now.Add(s.statusInterval)
		}

		rctx, rcancel := context.WithDeadline(ctx, nextStandby)
		raw, err := conn.ReceiveMessage(rctx)
		rcancel()
		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			s.fail(ctx, status, err)
			return
		}

		cd, ok := raw.(*pgproto3.CopyData)
		if !ok {
			continue
		}

		switch cd.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			ka, kaErr := pglogrepl.ParsePrimaryKeepaliveMessage(cd.Data[1:])
			if kaErr != nil {
				s.fail(ctx, status, kaErr)
				return
			}
			if ka.ServerWALEnd > clientPos {
				clientPos = ka.ServerWALEnd
			}
			if ka.ReplyRequested {
				if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
					pglogrepl.StandbyStatusUpdate{WALWritePosition: clientPos}); err != nil {
					s.fail(ctx, status, err)
					return
				}
			}
		case pglogrepl.XLogDataByteID:
			xld, xErr := pglogrepl.ParseXLogData(cd.Data[1:])
			if xErr != nil {
				s.fail(ctx, status, xErr)
				return
			}
			changes, dErr := dec.decode(xld.WALData, xld.WALStart)
			if dErr != nil {
				s.fail(ctx, status, dErr)
				return
			}
			for i := range changes {
				s.emitChange(ctx, changes[i])
			}
			if end := xld.WALStart + pglogrepl.LSN(len(xld.WALData)); end > clientPos {
				clientPos = end
			}
		}
	}
}

func checkpointAndAck(ctx context.Context, conn *pgconn.PgConn, cp Checkpointer, slot string, pos pglogrepl.LSN) error {
	if err := cp.Save(ctx, slot, pos); err != nil {
		return err
	}
	if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
		pglogrepl.StandbyStatusUpdate{WALWritePosition: pos}); err != nil {
		return fmt.Errorf("standby status update: %w", err)
	}
	return nil
}

func (s *Source) emitChange(ctx context.Context, c RowChange) {
	s.bus.Send(ctx, event.Event{
		System: s.eventSystem,
		Kind:   config.ChangeKind,
		Path:   c.Relation(),
		Data:   c,
	})
}

func (s *Source) reportLag(ctx context.Context, adminDB *sql.DB, mc metrics.Collector) {
	var retained int64
	err := adminDB.QueryRowContext(ctx,
		`SELECT COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0)::bigint
		   FROM pg_replication_slots WHERE slot_name = $1`, s.slot).Scan(&retained)
	if err != nil {
		s.log.Warn("cdc lag query failed", zap.String("slot", s.slot), zap.Error(err))
		return
	}
	if mc != nil {
		mc.GaugeSet(retainedWALGauge, float64(retained), metrics.Labels{"slot": s.slot})
	}
	s.bus.Send(ctx, event.Event{
		System: s.eventSystem,
		Kind:   config.StatusKind,
		Path:   s.slot,
		Data: map[string]any{
			"slot":               s.slot,
			"retained_wal_bytes": retained,
		},
	})
}

func (s *Source) fail(ctx context.Context, status chan any, err error) {
	s.log.Error("cdc stream error", zap.String("slot", s.slot), zap.Error(err))
	s.bus.Send(context.WithoutCancel(ctx), event.Event{
		System: s.eventSystem,
		Kind:   config.ErrorKind,
		Path:   s.slot,
		Data:   map[string]any{"slot": s.slot, "error": err.Error()},
	})
	select {
	case status <- err:
	default:
	}
}

func (s *Source) prepareSlot(
	ctx context.Context,
	conn *pgconn.PgConn,
	adminDB *sql.DB,
	cp Checkpointer,
	fallback pglogrepl.LSN,
) (pglogrepl.LSN, string, error) {
	var start pglogrepl.LSN
	resumed := false
	if cpLSN, ok, err := cp.Load(ctx, s.slot); err != nil {
		return 0, "", err
	} else if ok {
		start = cpLSN
		resumed = true
	}

	exists := false
	if !s.temporary {
		var err error
		exists, err = slotExists(ctx, adminDB, s.slot)
		if err != nil {
			return 0, "", err
		}
	}

	snapshotName := ""
	if !exists {
		opts := pglogrepl.CreateReplicationSlotOptions{Temporary: s.temporary}
		wantSnapshot := s.snapshot && !resumed
		if wantSnapshot {
			opts.SnapshotAction = "EXPORT_SNAPSHOT"
		}
		res, err := pglogrepl.CreateReplicationSlot(ctx, conn, s.slot, config.OutputPlugin, opts)
		if err != nil {
			return 0, "", fmt.Errorf("create replication slot: %w", err)
		}
		cpoint, err := pglogrepl.ParseLSN(res.ConsistentPoint)
		if err != nil {
			return 0, "", fmt.Errorf("parse consistent point %q: %w", res.ConsistentPoint, err)
		}
		if cpoint > start {
			start = cpoint
		}
		if wantSnapshot {
			snapshotName = res.SnapshotName
		}
	}

	if start == 0 {
		start = fallback
	}
	return start, snapshotName, nil
}

type tableRef struct {
	schema string
	name   string
}

func (t tableRef) quoted() string {
	return pq.QuoteIdentifier(t.schema) + "." + pq.QuoteIdentifier(t.name)
}

func (s *Source) snapshotExisting(ctx context.Context, adminDB *sql.DB, publication, snapshotName string) error {
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
		n, err := s.snapshotTable(ctx, conn, tbl)
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

func (s *Source) snapshotTable(ctx context.Context, conn *sql.Conn, tbl tableRef) (int, error) {
	if _, err := conn.ExecContext(ctx,
		"DECLARE "+snapshotCursor+" NO SCROLL CURSOR FOR SELECT * FROM "+tbl.quoted()); err != nil {
		return 0, fmt.Errorf("declare cursor %s.%s: %w", tbl.schema, tbl.name, err)
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), snapshotCloseSQL) }()

	n := 0
	for {
		got, err := s.fetchSnapshotBatch(ctx, conn, tbl)
		if err != nil {
			return n, err
		}
		n += got
		if got < snapshotFetchSize {
			return n, nil
		}
	}
}

func (s *Source) fetchSnapshotBatch(ctx context.Context, conn *sql.Conn, tbl tableRef) (int, error) {
	rows, err := conn.QueryContext(ctx, snapshotFetchSQL)
	if err != nil {
		return 0, fmt.Errorf("fetch %s.%s: %w", tbl.schema, tbl.name, err)
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("snapshot columns %s.%s: %w", tbl.schema, tbl.name, err)
	}

	got := 0
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
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
		s.emitChange(ctx, RowChange{Op: OpSnapshot, Schema: tbl.schema, Table: tbl.name, After: after})
		got++
	}
	return got, rows.Err()
}

func publishedTables(ctx context.Context, conn *sql.Conn, publication string) ([]tableRef, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT schemaname, tablename FROM pg_publication_tables WHERE pubname = $1
		 ORDER BY schemaname, tablename`, publication)
	if err != nil {
		return nil, fmt.Errorf("list published tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tables []tableRef
	for rows.Next() {
		var t tableRef
		if err := rows.Scan(&t.schema, &t.name); err != nil {
			return nil, fmt.Errorf("scan published table: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func slotExists(ctx context.Context, adminDB *sql.DB, slot string) (bool, error) {
	var n int
	err := adminDB.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_replication_slots WHERE slot_name = $1`, slot).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check slot existence: %w", err)
	}
	return n > 0, nil
}

func (s *Source) ensurePublication(ctx context.Context, adminDB *sql.DB) (string, error) {
	if s.publication != "" {
		return s.publication, nil
	}
	if len(s.tables) == 0 {
		return "", ErrNoPublication
	}
	name := s.slot + "_pub"

	var n int
	if err := adminDB.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication WHERE pubname = $1`, name).Scan(&n); err != nil {
		return "", fmt.Errorf("check publication: %w", err)
	}
	if n == 0 {
		quoted := make([]string, len(s.tables))
		for i, t := range s.tables {
			quoted[i] = quoteQualifiedIdent(t)
		}
		stmt := fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s",
			pq.QuoteIdentifier(name), strings.Join(quoted, ", "))
		if _, err := adminDB.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("create publication: %w", err)
		}
	}
	return name, nil
}

func (s *Source) abortFreshSnapshot(conn *pgconn.PgConn) {
	_ = conn.Close(context.Background())
	if s.temporary {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.dropSlotAndCheckpoint(cleanupCtx); err != nil {
		s.log.Warn("cdc cleanup after snapshot failure failed",
			zap.String("slot", s.slot), zap.Error(err))
	}
}

func (s *Source) dropSlotAndCheckpoint(ctx context.Context) error {
	adminDB, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		return fmt.Errorf("open admin connection for slot drop: %w", err)
	}
	defer func() { _ = adminDB.Close() }()

	if err := dropReplicationSlot(ctx, adminDB, s.slot); err != nil {
		return fmt.Errorf("drop replication slot %q: %w", s.slot, err)
	}
	s.log.Info("cdc dropped replication slot on delete", zap.String("slot", s.slot))

	if s.injectedCP != nil {
		if err := s.injectedCP.Delete(ctx, s.slot); err != nil {
			return fmt.Errorf("delete checkpoint: %w", err)
		}
		return nil
	}
	if _, err := adminDB.ExecContext(ctx, `DELETE FROM wippy_cdc_offsets WHERE slot = $1`, s.slot); err != nil {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	return nil
}

func dropReplicationSlot(ctx context.Context, adminDB *sql.DB, slot string) error {
	var lastErr error
	for attempt := 0; attempt < slotDropMaxAttempts; attempt++ {
		_, err := adminDB.ExecContext(ctx, `SELECT pg_drop_replication_slot($1)`, slot)
		if err == nil {
			return nil
		}
		lastErr = err

		var pqErr *pq.Error
		if !errors.As(err, &pqErr) || string(pqErr.Code) != slotActiveSQLState {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(slotDropRetryDelay):
		}
	}
	return lastErr
}

func quoteQualifiedIdent(name string) string {
	parts := strings.Split(name, ".")
	for i, p := range parts {
		parts[i] = pq.QuoteIdentifier(p)
	}
	return strings.Join(parts, ".")
}
