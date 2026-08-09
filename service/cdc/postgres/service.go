// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"crypto/sha256"
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

	"github.com/wippyai/runtime/api/metrics"
	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	retainedWALGauge        = "wippy_cdc_retained_wal_bytes"
	changesCounter          = "wippy_cdc_changes_total"
	errorsCounter           = "wippy_cdc_errors_total"
	transactionLimitCounter = "wippy_cdc_transaction_limit_total"
)

const (
	defaultStandbyInterval   = 10 * time.Second
	defaultStatusInterval    = 30 * time.Second
	defaultSnapshotFetchSize = 1000
	slotActiveSQLState       = "55006"
	slotDropMaxAttempts      = 10
	slotDropRetryDelay       = 100 * time.Millisecond
	snapshotCursor           = "wippy_cdc_snapshot"
	snapshotCloseSQL         = "CLOSE " + snapshotCursor
)

type SourceOptions struct {
	Checkpoint        Checkpointer
	Log               *zap.Logger
	ReplDSN           string
	AdminDSN          string
	Name              string
	Slot              string
	Publication       string
	Tables            []string
	StandbyInterval   time.Duration
	StatusInterval    time.Duration
	SnapshotFetchSize int
	Temporary         bool
	// Snapshot makes the atomic snapshot handoff the default for each
	// subscriber. It is an entry default; Start never emits a source-global
	// snapshot.
	Snapshot  bool
	Streaming bool
	Failover  bool
	// MaxTransactionChanges bounds the number of row changes retained before
	// an ordinary or streamed transaction commits. Zero uses the safe default.
	MaxTransactionChanges int
	// MaxTransactionBytes bounds the estimated memory retained for one
	// ordinary or streamed transaction. Zero uses the safe default.
	MaxTransactionBytes int64
	// MaxInflightChanges bounds all uncommitted row changes across interleaved
	// ordinary and streamed transactions. Zero uses the safe default.
	MaxInflightChanges int
	// MaxInflightBytes bounds estimated memory retained by all uncommitted
	// ordinary and streamed transactions. Zero uses the safe default.
	MaxInflightBytes int64
}

type Source struct {
	coll                  metrics.Collector
	injectedCP            Checkpointer
	sourceErr             error
	log                   *zap.Logger
	cancel                context.CancelFunc
	done                  chan struct{}
	subs                  map[uint64]*sourceSubscription
	streamNotify          chan struct{}
	snapshotGate          chan struct{} // one temporary logical snapshot per source
	replDSN               string
	adminDSN              string
	name                  string
	slot                  string
	publication           string
	tables                []string
	standbyInterval       time.Duration
	statusInterval        time.Duration
	nextSubID             uint64
	snapshotFetchSize     int
	maxTransactionChanges int
	maxTransactionBytes   int64
	maxInflightChanges    int
	maxInflightBytes      int64
	snapshotWG            sync.WaitGroup
	streamPosition        pglogrepl.LSN
	subMu                 sync.RWMutex
	mu                    sync.Mutex
	dropMu                sync.Mutex
	dropSlot              atomic.Bool
	dropDone              atomic.Bool
	temporary             bool
	snapshot              bool
	streaming             bool
	failover              bool
	permanentlyClosed     bool
	state                 sourceState
}

type sourceState uint8

const (
	sourceNew sourceState = iota
	sourceStarting
	sourceRunning
	sourceStopping
	sourceFailed
	sourceStopped
)

var snapshotFailpoint func() error

func (s *Source) MarkForSlotDrop() {
	s.dropSlot.Store(true)
	s.mu.Lock()
	s.permanentlyClosed = true
	s.mu.Unlock()
}

// Close permanently retires a source. Stop alone is restartable so a
// supervisor can recover a failed generation; callers removing a source from
// the registry should use Close when the instance must not be started again.
func (s *Source) Close(ctx context.Context) error {
	s.mu.Lock()
	s.permanentlyClosed = true
	s.mu.Unlock()
	return s.Stop(ctx)
}

func NewSource(opts SourceOptions) *Source {
	log := opts.Log
	if log == nil {
		log = zap.NewNop()
	}
	standby := opts.StandbyInterval
	if standby <= 0 {
		standby = defaultStandbyInterval
	}
	status := opts.StatusInterval
	if status <= 0 {
		status = defaultStatusInterval
	}
	fetch := opts.SnapshotFetchSize
	if fetch <= 0 {
		fetch = defaultSnapshotFetchSize
	}
	limits := normalizeDecoderLimits(decoderLimits{
		maxChanges:         opts.MaxTransactionChanges,
		maxBytes:           opts.MaxTransactionBytes,
		maxInflightChanges: opts.MaxInflightChanges,
		maxInflightBytes:   opts.MaxInflightBytes,
	})
	return &Source{
		log:                   log,
		injectedCP:            opts.Checkpoint,
		replDSN:               opts.ReplDSN,
		adminDSN:              opts.AdminDSN,
		name:                  opts.Name,
		slot:                  opts.Slot,
		publication:           opts.Publication,
		tables:                append([]string(nil), opts.Tables...),
		subs:                  make(map[uint64]*sourceSubscription),
		temporary:             opts.Temporary,
		snapshot:              opts.Snapshot,
		streaming:             opts.Streaming,
		failover:              opts.Failover,
		standbyInterval:       standby,
		statusInterval:        status,
		snapshotFetchSize:     fetch,
		maxTransactionChanges: limits.maxChanges,
		maxTransactionBytes:   limits.maxBytes,
		maxInflightChanges:    limits.maxInflightChanges,
		maxInflightBytes:      limits.maxInflightBytes,
		streamNotify:          make(chan struct{}),
		snapshotGate:          make(chan struct{}, 1),
	}
}

func (s *Source) Start(ctx context.Context) (<-chan any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	s.mu.Lock()
	if s.permanentlyClosed {
		s.mu.Unlock()
		cancel()
		return nil, ErrSourceClosed
	}
	switch s.state {
	case sourceStarting, sourceRunning:
		s.mu.Unlock()
		cancel()
		return nil, ErrSourceRunning
	case sourceStopping:
		s.mu.Unlock()
		cancel()
		return nil, ErrSourceStopping
	default:
		s.state = sourceStarting
		s.cancel = cancel
		s.done = done
		// A failed snapshot/start may have dropped and checkpoint-cleaned the
		// previous slot. This start may create a new slot generation, so the
		// destructive cleanup marker must apply to that generation as well.
		s.dropDone.Store(false)
	}
	s.mu.Unlock()

	failStart := func(startErr error) {
		cancel()
		s.mu.Lock()
		if s.done == done {
			switch s.state {
			case sourceStopping:
				s.state = sourceStopped
			case sourceStarting:
				s.state = sourceFailed
				s.sourceErr = startErr
			}
			s.cancel = nil
			close(done)
		}
		s.mu.Unlock()
	}

	adminDB, err := sql.Open("postgres", s.adminDSN)
	if err != nil {
		startErr := fmt.Errorf("open admin connection: %w", err)
		failStart(startErr)
		return nil, startErr
	}
	adminDB.SetMaxOpenConns(2)
	adminDB.SetMaxIdleConns(1)
	if err := adminDB.PingContext(runCtx); err != nil {
		_ = adminDB.Close()
		startErr := fmt.Errorf("ping admin connection: %w", err)
		failStart(startErr)
		return nil, startErr
	}

	cp := s.injectedCP
	if cp == nil {
		dbcp, cpErr := NewDBCheckpointer(runCtx, adminDB)
		if cpErr != nil {
			_ = adminDB.Close()
			failStart(cpErr)
			return nil, cpErr
		}
		cp = dbcp
	}

	publication, err := s.ensurePublication(runCtx, adminDB)
	if err != nil {
		_ = adminDB.Close()
		failStart(err)
		return nil, err
	}

	conn, err := pgconn.Connect(runCtx, s.replDSN)
	if err != nil {
		_ = adminDB.Close()
		startErr := fmt.Errorf("replication connect: %w", err)
		failStart(startErr)
		return nil, startErr
	}

	sysident, err := pglogrepl.IdentifySystem(runCtx, conn)
	if err != nil {
		_ = conn.Close(context.Background())
		_ = adminDB.Close()
		startErr := fmt.Errorf("identify system: %w", err)
		failStart(startErr)
		return nil, startErr
	}

	startLSN, slotCreated, err := s.prepareSlot(runCtx, conn, adminDB, cp, sysident.XLogPos)
	if err != nil {
		_ = conn.Close(context.Background())
		_ = adminDB.Close()
		if slotCreated {
			s.cleanupFreshSlot()
		}
		failStart(err)
		return nil, err
	}

	status := make(chan any, 8)

	s.mu.Lock()
	if s.state != sourceStarting {
		// Stop may have been requested while the synchronous connection and
		// slot setup was in progress. Do not publish a source that is already
		// being stopped.
		s.mu.Unlock()
		_ = conn.Close(context.Background())
		_ = adminDB.Close()
		if slotCreated {
			s.cleanupFreshSlot()
		}
		failStart(ErrSourceClosed)
		return nil, ErrSourceClosed
	}
	s.state = sourceRunning
	s.sourceErr = nil
	s.publication = publication
	s.streamPosition = startLSN
	if s.streamNotify == nil {
		s.streamNotify = make(chan struct{})
	}
	s.mu.Unlock()

	s.log.Info("cdc source started",
		zap.String("slot", s.slot),
		zap.String("publication", publication),
		zap.String("start_lsn", startLSN.String()),
		zap.Bool("snapshot", s.snapshot))
	select {
	case status <- "cdc replication started":
	default:
	}

	s.coll = metrics.GetCollector(runCtx)
	go s.run(runCtx, conn, adminDB, cp, startLSN, slotCreated, publication, s.coll, status, done)
	return status, nil
}

func (s *Source) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.state == sourceStopped {
		drop := s.dropSlot.Load()
		s.mu.Unlock()
		if drop {
			return s.dropSlotAndCheckpoint(ctx)
		}
		return nil
	}
	if s.state == sourceNew || s.state == sourceFailed {
		if s.state == sourceNew {
			// Keep the generation stopping until all snapshot workers have
			// joined. This prevents a concurrent Start from resetting state
			// while a worker can still call WaitGroup.Done.
			s.state = sourceStopping
			s.cancel = nil
			s.mu.Unlock()
			s.closeSubscriptions()
			if err := s.waitSnapshots(ctx); err != nil {
				return err
			}
			s.mu.Lock()
			if s.state == sourceStopping {
				s.state = sourceStopped
			}
			s.mu.Unlock()
			if s.dropSlot.Load() {
				return s.dropSlotAndCheckpoint(ctx)
			}
			return nil
		}
		// A replication run marks the source failed before its deferred
		// cleanup closes done. Keep the source stopping until that run has
		// fully released its connections; replacement and slot deletion must
		// not race the failed generation.
		s.state = sourceStopping
	}
	if s.state == sourceStarting || s.state == sourceRunning {
		s.state = sourceStopping
	}
	cancel := s.cancel
	done := s.done
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.closeSubscriptions()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	} else {
		s.mu.Lock()
		if s.state == sourceStopping {
			s.state = sourceStopped
			s.cancel = nil
		}
		s.mu.Unlock()
	}
	if err := s.waitSnapshots(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.state == sourceStopping {
		s.state = sourceStopped
		s.cancel = nil
	}
	s.mu.Unlock()

	if s.dropSlot.Load() {
		return s.dropSlotAndCheckpoint(ctx)
	}
	return nil
}

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

// advanceStreamPosition publishes the replication receive watermark after a
// complete XLogData message has been decoded and emitted, or after a server
// keepalive reports its WAL end. Snapshot handoff waits for this watermark
// before releasing its pending live queue, so a change at or before the
// exported snapshot fence cannot arrive late and be duplicated after the
// handoff. It never updates the transaction-safe checkpoint.
func (s *Source) advanceStreamPosition(done chan struct{}, position pglogrepl.LSN) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != done || position <= s.streamPosition {
		return
	}
	s.streamPosition = position
	close(s.streamNotify)
	s.streamNotify = make(chan struct{})
}

func (s *Source) observeKeepalive(done chan struct{}, keepalive pglogrepl.PrimaryKeepaliveMessage) {
	s.advanceStreamPosition(done, keepalive.ServerWALEnd)
}

func (s *Source) waitStreamPosition(ctx context.Context, fence pglogrepl.LSN) error {
	for {
		s.mu.Lock()
		if s.streamPosition >= fence {
			s.mu.Unlock()
			return nil
		}
		notify := s.streamNotify
		s.mu.Unlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Source) finishRunGeneration(done chan struct{}) (bool, []*sourceSubscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != done {
		return false, nil
	}
	switch s.state {
	case sourceRunning, sourceStarting:
		s.state = sourceFailed
	}
	s.cancel = nil
	return true, s.detachSubscriptionsLocked()
}

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
			if result.safe {
				if end := xld.WALStart + pglogrepl.LSN(len(xld.WALData)); end > safePos {
					safePos = end
				}
			}
			s.advanceStreamPosition(done, xld.WALStart+pglogrepl.LSN(len(xld.WALData)))
		default:
			s.fail(ctx, status, fmt.Errorf("%w: copy data kind %q", ErrUnsupportedMessage, cd.Data[0]))
			return
		}
	}
}

func (s *Source) emitChange(ctx context.Context, c RowChange) {
	s.publishChange(ctx, config.Change{
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
