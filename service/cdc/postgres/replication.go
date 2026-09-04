// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/wippyai/runtime/api/metrics"
	config "github.com/wippyai/runtime/api/service/cdc"
	"go.uber.org/zap"
)

func (s *Source) run(
	ctx context.Context,
	conn *pgconn.PgConn,
	adminDB *sql.DB,
	cp Checkpointer,
	startLSN pglogrepl.LSN,
	slotCreated bool,
	publication string,
	mc metrics.Collector,
	status chan any,
	done chan struct{},
) {
	defer func() {
		current, subs := s.finishRunGeneration(done)
		if current {
			s.closeDetachedSubscriptions(subs, nil)
		}
		close(done)
	}()
	defer close(status)
	defer func() { _ = adminDB.Close() }()
	defer func() { _ = conn.Close(context.Background()) }()

	protoVersion := config.ProtocolVersion
	if s.streaming {
		protoVersion = config.StreamingProtocolVersion
	}
	publicationLiteral, err := quotePostgresLiteral(publication, "publication")
	if err != nil {
		s.abortFreshSlot(conn, slotCreated)
		s.fail(ctx, status, err)
		return
	}
	pluginArgs := []string{
		fmt.Sprintf("proto_version '%d'", protoVersion),
		fmt.Sprintf("publication_names %s", publicationLiteral),
	}
	if s.streaming {
		pluginArgs = append(pluginArgs, "streaming 'on'")
	}
	slotIdentifier, err := quoteReplicationSlotName(s.slot)
	if err != nil {
		s.abortFreshSlot(conn, slotCreated)
		s.fail(ctx, status, err)
		return
	}
	if err := pglogrepl.StartReplication(ctx, conn, slotIdentifier, startLSN,
		pglogrepl.StartReplicationOptions{PluginArgs: pluginArgs}); err != nil {
		s.abortFreshSlot(conn, slotCreated)
		s.fail(ctx, status, err)
		return
	}

	limits := decoderLimits{
		maxChanges:         s.maxTransactionChanges,
		maxBytes:           s.maxTransactionBytes,
		maxInflightChanges: s.maxInflightChanges,
		maxInflightBytes:   s.maxInflightBytes,
	}
	dec := newDecoder(limits)
	if s.streaming {
		dec = newStreamingDecoder(limits)
	}

	var opLabels map[Op]metrics.Labels
	if mc != nil {
		opLabels = map[Op]metrics.Labels{
			OpInsert:   {"source": s.name, "op": string(OpInsert)},
			OpUpdate:   {"source": s.name, "op": string(OpUpdate)},
			OpDelete:   {"source": s.name, "op": string(OpDelete)},
			OpTruncate: {"source": s.name, "op": string(OpTruncate)},
			OpSnapshot: {"source": s.name, "op": string(OpSnapshot)},
		}
	}

	// safePos is the furthest position that can be replayed without losing
	// decoder state. It advances only after a complete transaction boundary;
	// the server WAL end in a keepalive is never a safe checkpoint.
	safePos := startLSN
	lastSaved := startLSN
	defer func() {
		if safePos <= lastSaved {
			return
		}
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cp.Save(flushCtx, s.slot, safePos); err != nil {
			s.log.Warn("failed to persist final cdc checkpoint",
				zap.String("slot", s.slot), zap.String("lsn", safePos.String()), zap.Error(err))
		}
	}()
	saveSafe := func() error {
		if safePos <= lastSaved {
			return nil
		}
		if err := cp.Save(ctx, s.slot, safePos); err != nil {
			return err
		}
		lastSaved = safePos
		return nil
	}
	now := time.Now()
	nextStandby := now.Add(s.standbyInterval)
	nextStatus := now.Add(s.statusInterval)

	for {
		if ctx.Err() != nil {
			return
		}

		now = time.Now()
		if !now.Before(nextStandby) {
			if err := saveSafe(); err != nil {
				s.fail(ctx, status, err)
				return
			}
			if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
				pglogrepl.StandbyStatusUpdate{
					WALWritePosition: safePos,
					WALFlushPosition: safePos,
					WALApplyPosition: safePos,
				}); err != nil {
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
		if len(cd.Data) == 0 {
			s.fail(ctx, status, fmt.Errorf("%w: empty CopyData payload", ErrUnsupportedMessage))
			return
		}

		switch cd.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			ka, kaErr := pglogrepl.ParsePrimaryKeepaliveMessage(cd.Data[1:])
			if kaErr != nil {
				s.fail(ctx, status, kaErr)
				return
			}
			// ServerWALEnd is the receive watermark used by an in-flight
			// snapshot handoff. It is deliberately independent from safePos:
			// keepalives must not advance the transaction-safe checkpoint.
			s.observeKeepalive(done, ka)
			if ka.ReplyRequested {
				if err := saveSafe(); err != nil {
					s.fail(ctx, status, err)
					return
				}
				if err := pglogrepl.SendStandbyStatusUpdate(ctx, conn,
					pglogrepl.StandbyStatusUpdate{
						WALWritePosition: safePos,
						WALFlushPosition: safePos,
						WALApplyPosition: safePos,
					}); err != nil {
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
			result, dErr := dec.decodeResult(xld.WALData, xld.WALStart)
			if dErr != nil {
				s.fail(ctx, status, dErr)
				return
			}
			for i := range result.changes {
				s.emitChange(ctx, result.changes[i])
				if mc != nil {
					mc.CounterInc(changesCounter, opLabels[result.changes[i].Op])
				}
			}
			if result.safe && result.position > safePos {
				safePos = result.position
			}
			s.advanceStreamPosition(done, xld.WALStart)
		default:
			s.fail(ctx, status, fmt.Errorf("%w: copy data kind %q", ErrUnsupportedMessage, cd.Data[0]))
			return
		}
	}
}

func (s *Source) emitChange(ctx context.Context, c RowChange) {
	s.publishChange(ctx, config.Change{
		Unchanged: c.Unchanged,
		Source:    s.name,
		Op:        string(c.Op),
		Schema:    c.Schema,
		Table:     c.Table,
		Relation:  c.Relation(),
		LSN:       c.LSN,
		CommitLSN: c.CommitLSN,
		XID:       c.XID,
		Before:    c.Before,
		After:     c.After,
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
		mc.GaugeSet(retainedWALGauge, float64(retained), metrics.Labels{"source": s.name})
	}
}

func (s *Source) fail(_ context.Context, status chan any, err error) {
	if err == nil {
		err = ErrSourceClosed
	}
	s.mu.Lock()
	s.sourceErr = err
	if s.state == sourceRunning || s.state == sourceStarting {
		s.state = sourceFailed
	}
	s.mu.Unlock()
	s.log.Error("cdc stream error", zap.String("slot", s.slot), zap.Error(err))
	if s.coll != nil {
		s.coll.CounterInc(errorsCounter, metrics.Labels{"source": s.name})
		if errors.Is(err, ErrTransactionLimit) {
			s.coll.CounterInc(transactionLimitCounter, metrics.Labels{"source": s.name})
		}
	}
	s.closeSubscriptionsWithError(err)
	select {
	case status <- err:
	default:
	}
}
