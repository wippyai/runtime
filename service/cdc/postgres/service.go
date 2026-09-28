// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap"
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
