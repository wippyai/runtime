// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	config "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

func (s *Source) prepareSlot(
	ctx context.Context,
	conn *pgconn.PgConn,
	adminDB *sql.DB,
	cp Checkpointer,
	fallback pglogrepl.LSN,
) (pglogrepl.LSN, bool, error) {
	var start pglogrepl.LSN
	resumed := false
	if cpLSN, ok, err := cp.Load(ctx, s.slot); err != nil {
		return 0, false, err
	} else if ok {
		start = cpLSN
		resumed = true
	}

	exists := false
	if !s.temporary {
		var err error
		exists, err = slotExists(ctx, adminDB, s.slot)
		if err != nil {
			return 0, false, err
		}
		if !exists && resumed {
			// A local offset is meaningful only for the server-side slot
			// incarnation that produced it. If that slot disappeared, do not
			// reuse the old LSN for a newly-created slot.
			if err := cp.Delete(ctx, s.slot); err != nil {
				return 0, false, fmt.Errorf("delete stale cdc checkpoint: %w", err)
			}
			start = 0
			resumed = false
		}
		if exists && !resumed {
			// A persistent slot is the server-side durable cursor. Never fall
			// back to the current system WAL position when local checkpoint
			// state is missing; doing so can skip retained logical changes.
			confirmed, valid, err := slotConfirmedFlush(ctx, adminDB, s.slot)
			if err != nil {
				return 0, false, err
			}
			if valid {
				start = confirmed
			}
		}
	} else if resumed {
		// Temporary slots are destroyed with their replication connection, so
		// any persisted offset belongs to an older slot incarnation.
		if err := cp.Delete(ctx, s.slot); err != nil {
			return 0, false, fmt.Errorf("delete stale cdc checkpoint: %w", err)
		}
		start = 0
	}

	slotCreated := false
	if !exists {
		slotIdentifier, err := quoteReplicationSlotName(s.slot)
		if err != nil {
			return 0, false, err
		}
		opts := pglogrepl.CreateReplicationSlotOptions{Temporary: s.temporary}
		res, err := pglogrepl.CreateReplicationSlot(ctx, conn, slotIdentifier, config.OutputPlugin, opts)
		if err != nil {
			return 0, false, fmt.Errorf("create replication slot: %w", err)
		}
		slotCreated = true
		cpoint, err := pglogrepl.ParseLSN(res.ConsistentPoint)
		if err != nil {
			return 0, slotCreated, fmt.Errorf("parse consistent point %q: %w", res.ConsistentPoint, err)
		}
		if cpoint > start {
			start = cpoint
		}
	}

	if s.failover && !s.temporary {
		if err := s.setSlotFailover(ctx, conn); err != nil {
			return 0, slotCreated, err
		}
	}

	if start == 0 {
		start = fallback
	}
	return start, slotCreated, nil
}

func (s *Source) setSlotFailover(ctx context.Context, conn *pgconn.PgConn) error {
	slotIdentifier, err := quoteReplicationSlotName(s.slot)
	if err != nil {
		return err
	}
	cmd := fmt.Sprintf("ALTER_REPLICATION_SLOT %s ( FAILOVER )", slotIdentifier)
	if err := conn.Exec(ctx, cmd).Close(); err != nil {
		return fmt.Errorf("set slot failover: %w", err)
	}
	s.log.Info("cdc slot marked for failover", zap.String("slot", s.slot))
	return nil
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

func slotConfirmedFlush(ctx context.Context, adminDB *sql.DB, slot string) (pglogrepl.LSN, bool, error) {
	var raw sql.NullString
	err := adminDB.QueryRowContext(ctx,
		`SELECT confirmed_flush_lsn::text
		   FROM pg_replication_slots
		  WHERE slot_name = $1`, slot).Scan(&raw)
	if err != nil {
		return 0, false, fmt.Errorf("read slot confirmed flush position: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return 0, false, nil
	}
	lsn, err := pglogrepl.ParseLSN(raw.String)
	if err != nil {
		return 0, false, fmt.Errorf("parse slot confirmed flush position %q: %w", raw.String, err)
	}
	return lsn, true, nil
}

func (s *Source) ensurePublication(ctx context.Context, adminDB *sql.DB) (string, error) {
	if s.publication != "" {
		if err := validatePostgresIdentifier(s.publication, "publication"); err != nil {
			return "", err
		}
		// pgoutput may defer publication lookup until the first WAL change.
		// Reject a missing publication before reporting startup or creating a
		// durable slot that would otherwise retain WAL while unable to decode.
		var exists bool
		if err := adminDB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname=$1)`, s.publication).Scan(&exists); err != nil {
			return "", fmt.Errorf("check publication: %w", err)
		}
		if !exists {
			return "", fmt.Errorf("publication %q does not exist", s.publication)
		}
		return s.publication, nil
	}
	if len(s.tables) == 0 {
		return "", ErrNoPublication
	}
	name := s.slot + "_pub"
	quotedName, err := quotePostgresIdentifier(name, "publication")
	if err != nil {
		return "", err
	}
	quotedTables := make([]string, 0, len(s.tables))
	seenTables := make(map[string]struct{}, len(s.tables))
	for _, table := range s.tables {
		quotedTable, err := quoteQualifiedIdent(table)
		if err != nil {
			return "", err
		}
		if _, exists := seenTables[quotedTable]; exists {
			continue
		}
		seenTables[quotedTable] = struct{}{}
		quotedTables = append(quotedTables, quotedTable)
	}
	if len(quotedTables) == 0 {
		return "", ErrNoPublication
	}

	var n int
	if err := adminDB.QueryRowContext(ctx,
		`SELECT count(*) FROM pg_publication WHERE pubname = $1`, name).Scan(&n); err != nil {
		return "", fmt.Errorf("check publication: %w", err)
	}
	if n == 0 {
		stmt := fmt.Sprintf("CREATE PUBLICATION %s FOR TABLE %s",
			quotedName, strings.Join(quotedTables, ", "))
		if _, err := adminDB.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("create publication: %w", err)
		}
	} else {
		// The generated name is owned by this source configuration. Reconcile
		// its membership exactly on every start so an update cannot silently
		// continue publishing an old table set. User-supplied publications take
		// the early return above and are never altered or dropped.
		stmt := fmt.Sprintf("ALTER PUBLICATION %s SET TABLE %s",
			quotedName, strings.Join(quotedTables, ", "))
		if _, err := adminDB.ExecContext(ctx, stmt); err != nil {
			return "", fmt.Errorf("reconcile publication: %w", err)
		}
	}
	return name, nil
}

func (s *Source) abortFreshSlot(conn *pgconn.PgConn, created bool) {
	if conn != nil {
		_ = conn.Close(context.Background())
	}
	if !created {
		return
	}
	s.cleanupFreshSlot()
}

func (s *Source) cleanupFreshSlot() {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.dropSlotAndCheckpoint(cleanupCtx); err != nil {
		s.log.Warn("cdc cleanup after fresh slot failure failed",
			zap.String("slot", s.slot), zap.Error(err))
	}
}

func (s *Source) dropSlotAndCheckpoint(ctx context.Context) error {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	if s.dropDone.Load() {
		return nil
	}

	adminDB, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		return fmt.Errorf("open admin connection for slot drop: %w", err)
	}
	adminDB.SetMaxOpenConns(1)
	defer func() { _ = adminDB.Close() }()

	if err := dropReplicationSlot(ctx, adminDB, s.slot); err != nil {
		return fmt.Errorf("drop replication slot %q: %w", s.slot, err)
	}
	s.log.Info("cdc dropped replication slot on delete", zap.String("slot", s.slot))

	if s.injectedCP != nil {
		if err := s.injectedCP.Delete(ctx, s.slot); err != nil {
			return fmt.Errorf("delete checkpoint: %w", err)
		}
		s.dropDone.Store(true)
		return nil
	}
	if _, err := adminDB.ExecContext(ctx, `DELETE FROM wippy_cdc_offsets WHERE slot = $1`, s.slot); err != nil {
		return fmt.Errorf("delete checkpoint: %w", err)
	}
	s.dropDone.Store(true)
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
		if errors.As(err, &pqErr) && string(pqErr.Code) == "42704" {
			// Delete is intentionally idempotent. A source can have already
			// dropped its slot during Stop before the manager retries Dispose.
			return nil
		}
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

func quoteQualifiedIdent(name string) (string, error) {
	parts := strings.Split(name, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return "", fmt.Errorf("%w: table", ErrInvalidIdentifier)
	}
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quotedPart, err := quotePostgresIdentifier(p, "table")
		if err != nil {
			return "", err
		}
		quoted[i] = quotedPart
	}
	return strings.Join(quoted, "."), nil
}
