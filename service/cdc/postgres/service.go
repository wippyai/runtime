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
	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	defaultStandbyInterval = 10 * time.Second
	defaultStatusInterval  = 30 * time.Second
	slotActiveSQLState     = "55006"
	slotDropMaxAttempts    = 10
	slotDropRetryDelay     = 100 * time.Millisecond
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
	stopped         atomic.Bool
	dropSlot        atomic.Bool
}

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

	startLSN, err := s.prepareSlot(ctx, conn, adminDB, cp, sysident.XLogPos)
	if err != nil {
		_ = conn.Close(ctx)
		_ = adminDB.Close()
		return nil, err
	}

	pluginArgs := []string{
		fmt.Sprintf("proto_version '%d'", config.ProtocolVersion),
		fmt.Sprintf("publication_names '%s'", publication),
	}
	if err := pglogrepl.StartReplication(ctx, conn, s.slot, startLSN,
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		_ = conn.Close(ctx)
		_ = adminDB.Close()
		return nil, fmt.Errorf("start replication: %w", err)
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
		zap.String("start_lsn", startLSN.String()))
	select {
	case status <- "cdc replication started":
	default:
	}

	go s.run(runCtx, conn, adminDB, cp, startLSN, status, done)
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
	status chan any,
	done chan struct{},
) {
	defer close(done)
	defer close(status)
	defer func() { _ = adminDB.Close() }()
	defer func() { _ = conn.Close(context.Background()) }()

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
			s.reportLag(ctx, adminDB)
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

func (s *Source) reportLag(ctx context.Context, adminDB *sql.DB) {
	var retained int64
	err := adminDB.QueryRowContext(ctx,
		`SELECT COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn), 0)::bigint
		   FROM pg_replication_slots WHERE slot_name = $1`, s.slot).Scan(&retained)
	if err != nil {
		s.log.Warn("cdc lag query failed", zap.String("slot", s.slot), zap.Error(err))
		return
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
) (pglogrepl.LSN, error) {
	var start pglogrepl.LSN
	if cpLSN, ok, err := cp.Load(ctx, s.slot); err != nil {
		return 0, err
	} else if ok {
		start = cpLSN
	}

	exists := false
	if !s.temporary {
		var err error
		exists, err = slotExists(ctx, adminDB, s.slot)
		if err != nil {
			return 0, err
		}
	}

	if !exists {
		res, err := pglogrepl.CreateReplicationSlot(ctx, conn, s.slot, config.OutputPlugin,
			pglogrepl.CreateReplicationSlotOptions{Temporary: s.temporary})
		if err != nil {
			return 0, fmt.Errorf("create replication slot: %w", err)
		}
		cpoint, err := pglogrepl.ParseLSN(res.ConsistentPoint)
		if err != nil {
			return 0, fmt.Errorf("parse consistent point %q: %w", res.ConsistentPoint, err)
		}
		if cpoint > start {
			start = cpoint
		}
	}

	if start == 0 {
		start = fallback
	}
	return start, nil
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
