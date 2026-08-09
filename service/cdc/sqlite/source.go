// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/cdc"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/api/supervisor"
	sqlservice "github.com/wippyai/runtime/service/sql"
)

const (
	defaultStatusInterval = 30 * time.Second
	cleanupTimeout        = 5 * time.Second
	changesCounter        = "wippy_cdc_changes_total"
)

// Source is the SQLite CDC adapter. The SQL resource owns the SQLite
// connection, driver hooks, and observer lifetime. This adapter only borrows
// the resource long enough to subscribe to its committed-mutation capability;
// it never opens another connection and never installs hooks on a raw one.
type Source struct {
	sourceErr      error
	res            resource.Registry
	observerSource sqlapi.CommittedMutationSource
	observer       sqlapi.MutationStream
	status         chan any
	runCancel      context.CancelFunc
	stopGate       chan struct{}
	startCancel    context.CancelFunc
	subs           *subscribers
	log            *zap.Logger
	startDone      chan struct{}
	snapshotSubs   map[*subscription]sqlapi.MutationStream
	runDone        chan struct{}
	snapshotWait   chan struct{}
	snapshotAcq    map[uint64]*snapshotAcquisition
	id             registry.ID
	dbResID        registry.ID
	name           string
	state          config.SourceState
	generation     string
	tables         []string
	lifecycle      configLifecycle
	snapshotWG     sync.WaitGroup
	statusTick     time.Duration
	nextSnapshotID uint64
	mu             sync.RWMutex
	snapshot       bool
	statusClosed   bool
	stopping       bool
}

// configLifecycle is an alias kept local to this package so the Source does
// not expose configuration implementation details through its API.
type configLifecycle = supervisor.LifecycleConfig

type snapshotAcquisition struct {
	cancel context.CancelFunc
}

func buildSource(opts sourceOptions) (managedSource, error) {
	log := opts.log
	if log == nil {
		log = zap.NewNop()
	}
	interval := defaultStatusInterval
	if opts.statusInterval != "" {
		d, err := time.ParseDuration(opts.statusInterval)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("invalid status_interval %q", opts.statusInterval)
		}
		if d > 0 {
			interval = d
		}
	}
	name := opts.name
	if name == "" && (opts.id.NS != "" || opts.id.Name != "") {
		name = opts.id.String()
	}
	if name == "" {
		name = "sqlite"
	}
	stopGate := make(chan struct{}, 1)
	stopGate <- struct{}{}

	return &Source{
		res:          opts.res,
		log:          log,
		id:           opts.id,
		name:         name,
		dbResID:      opts.dbResource,
		tables:       append([]string(nil), opts.tables...),
		statusTick:   interval,
		lifecycle:    opts.lifecycle,
		snapshot:     opts.snapshot,
		subs:         newSubscribers(),
		snapshotSubs: make(map[*subscription]sqlapi.MutationStream),
		snapshotAcq:  make(map[uint64]*snapshotAcquisition),
		stopGate:     stopGate,
		state:        config.SourceStateUnknown,
	}, nil
}

// Info reports the guarantees of this generation. SQLite observer positions
// are process-local and are not a durable LSN or resumable checkpoint. The
// SQL-owned snapshot stream provides an atomic snapshot/live handoff; writes
// made through a different unobserved database generation are not captured.
func (s *Source) Info() config.SourceInfo {
	s.mu.RLock()
	state := s.state
	generation := s.generation
	err := s.sourceErr
	s.mu.RUnlock()

	info := config.SourceInfo{
		ID:         s.id,
		Kind:       config.SQLite,
		State:      state,
		Generation: generation,
		Name:       s.name,
		Engine:     "sqlite",
		DBResource: s.dbResID.String(),
		Tables:     append([]string(nil), s.tables...),
		Epoch:      generation,
		Capabilities: config.Capabilities{
			Snapshot:               true,
			Durable:                false,
			Replayable:             false,
			CapturesExternalWrites: false,
			BeforeImages:           true,
			Coalesced:              true,
		},
		Snapshot:  true,
		Streaming: state == config.SourceStateRunning,
		Faulted:   state == config.SourceStateFaulted,
	}
	if err != nil {
		info.Error = err.Error()
	}
	return info
}

// LifecycleConfig lets the generic CDC manager register this source with the
// platform supervisor. The source itself does not emit lifecycle events.
func (s *Source) LifecycleConfig() supervisor.LifecycleConfig { return s.lifecycle }

// Start subscribes to the SQL resource's observer and starts the forwarding
// loop. The resource borrow is released immediately after Subscribe succeeds;
// the SQL generation remains the owner of the observer and closes it when the
// database generation is replaced or stopped.
func (s *Source) Start(ctx context.Context) (<-chan any, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return nil, ErrSourceClosed
	}
	if s.state == config.SourceStateRunning {
		status := s.status
		s.mu.Unlock()
		return status, nil
	}
	if s.state == config.SourceStateStarting {
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: start already in progress", config.ErrSourceNotReady)
	}
	startCtx, startCancel := context.WithCancel(ctx)
	startDone := make(chan struct{})
	status := make(chan any, 8)
	s.state = config.SourceStateStarting
	s.stopping = false
	s.startCancel = startCancel
	s.startDone = startDone
	s.status = status
	s.statusClosed = false
	s.mu.Unlock()
	defer close(startDone)

	observer, err := s.acquireObserver(startCtx)
	if err != nil {
		startCancel()
		s.mu.Lock()
		s.sourceErr = err
		if !s.stopping {
			s.state = config.SourceStateFaulted
		}
		s.startCancel = nil
		if !s.stopping {
			s.closeStatusLocked()
		}
		s.mu.Unlock()
		return nil, err
	}

	stream, err := observer.Subscribe(startCtx, sqlapi.MutationOptions{
		Tables: append([]string(nil), s.tables...),
	})
	// Releasing the ordinary resource borrow is part of acquireObserver; the
	// observer remains owned by the SQL resource generation.
	if err != nil {
		startCancel()
		s.mu.Lock()
		s.sourceErr = err
		if !s.stopping {
			s.state = config.SourceStateFaulted
		}
		s.startCancel = nil
		if !s.stopping {
			s.closeStatusLocked()
		}
		s.mu.Unlock()
		return nil, fmt.Errorf("subscribe sqlite mutation observer: %w", err)
	}

	runCtx, runCancel := context.WithCancel(startCtx)
	runDone := make(chan struct{})

	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		runCancel()
		_ = stream.Close()
		startCancel()
		return nil, ErrSourceClosed
	}
	s.observer = stream
	s.observerSource = observer
	s.sourceErr = nil
	s.runCancel = runCancel
	s.runDone = runDone
	s.startCancel = nil
	s.state = config.SourceStateRunning
	s.mu.Unlock()

	select {
	case status <- "sqlite cdc source started":
	default:
	}
	go s.run(runCtx, stream, runDone)
	return status, nil
}

func (s *Source) acquireObserver(ctx context.Context) (sqlapi.CommittedMutationSource, error) {
	if s.res == nil {
		return nil, ErrResourceRegRequired
	}
	borrow, err := s.res.Acquire(ctx, s.dbResID, resource.ModeNormal)
	if err != nil {
		return nil, fmt.Errorf("acquire db resource: %w", err)
	}
	defer borrow.Release()

	value, err := borrow.Get()
	if err != nil {
		return nil, fmt.Errorf("get db resource: %w", err)
	}
	db, ok := value.(sqlservice.DBResource)
	if !ok {
		return nil, fmt.Errorf("resource %s is not a database", s.name)
	}
	if db.Type != sqlapi.SQLite {
		return nil, fmt.Errorf("resource %s is not a sqlite database (kind %s)", s.name, db.Type)
	}
	if db.Observer == nil {
		return nil, fmt.Errorf("resource %s does not expose committed mutation observation", s.name)
	}
	return db.Observer, nil
}

func (s *Source) run(ctx context.Context, stream sqlapi.MutationStream, done chan struct{}) {
	defer close(done)
	defer func() {
		s.mu.RLock()
		running := s.state == config.SourceStateRunning && !s.stopping
		s.mu.RUnlock()
		if running {
			s.fail(ErrSourceClosed)
		}
	}()

	collector := metrics.GetCollector(ctx)
	ticker := time.NewTicker(s.statusTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.mu.RLock()
			stopping := s.stopping || s.state == config.SourceStateStopped
			s.mu.RUnlock()
			if !stopping {
				s.fail(ctx.Err())
			}
			return
		case <-ticker.C:
			// The stream is deliberately passive. A status tick keeps the
			// lifecycle channel alive without probing a second SQL connection.
		case batch, ok := <-stream.Changes():
			if !ok {
				err := stream.Err()
				if err == nil {
					err = ErrSourceClosed
				}
				s.fail(err)
				return
			}
			if err := s.processBatch(batch, collector); err != nil {
				s.fail(err)
				return
			}
		}
	}
}

func (s *Source) processBatch(batch sqlapi.MutationBatch, collector metrics.Collector) error {
	if batch.Transaction == "" {
		return errors.New("sqlite mutation observer emitted a batch without a transaction identity")
	}
	for i, mutation := range batch.Changes {
		change, err := s.changeFromMutation(batch, i, mutation)
		if err != nil {
			return err
		}
		s.subs.publish(change)
		if collector != nil {
			collector.CounterInc(changesCounter, metrics.Labels{"source": s.name, "op": change.Op})
		}
	}
	return nil
}

func (s *Source) changeFromMutation(batch sqlapi.MutationBatch, index int, mutation sqlapi.Mutation) (config.Change, error) {
	op := strings.ToLower(strings.TrimSpace(mutation.Op))
	if op != "insert" && op != "update" && op != "delete" && (!batch.Snapshot || op != "snapshot") {
		return config.Change{}, fmt.Errorf("sqlite mutation observer emitted unsupported operation %q", mutation.Op)
	}
	if mutation.Table == "" {
		return config.Change{}, errors.New("sqlite mutation observer emitted a mutation without a table")
	}
	if len(mutation.Columns) == 0 && (len(mutation.Before) != 0 || len(mutation.After) != 0) {
		return config.Change{}, fmt.Errorf("sqlite mutation observer emitted %s.%s without captured columns", mutation.Schema, mutation.Table)
	}
	before, err := valuesByColumn(mutation.Columns, mutation.Before)
	if err != nil {
		return config.Change{}, fmt.Errorf("sqlite %s.%s before image: %w", mutation.Schema, mutation.Table, err)
	}
	after, err := valuesByColumn(mutation.Columns, mutation.After)
	if err != nil {
		return config.Change{}, fmt.Errorf("sqlite %s.%s after image: %w", mutation.Schema, mutation.Table, err)
	}

	cursor := batch.Transaction + "/" + strconv.Itoa(index)
	schema := normalizeSchema(mutation.Schema)
	return config.Change{
		Before:      before,
		After:       after,
		Source:      s.name,
		SourceID:    s.id,
		Op:          op,
		Schema:      schema,
		Table:       mutation.Table,
		Relation:    mutation.Table,
		Cursor:      cursor,
		Transaction: batch.Transaction,
	}, nil
}

func valuesByColumn(columns []string, values []any) (map[string]any, error) {
	if values == nil {
		return nil, nil
	}
	if len(columns) != len(values) {
		return nil, fmt.Errorf("captured %d values for %d columns", len(values), len(columns))
	}
	out := make(map[string]any, len(values))
	for i, column := range columns {
		column = strings.TrimSpace(column)
		if column == "" {
			return nil, fmt.Errorf("captured column %d has an empty name", i)
		}
		if _, exists := out[column]; exists {
			return nil, fmt.Errorf("captured duplicate column %q", column)
		}
		out[column] = cloneValue(values[i])
	}
	return out, nil
}

func cloneValue(value any) any {
	bytes, ok := value.([]byte)
	if !ok {
		return value
	}
	return append([]byte(nil), bytes...)
}

func normalizeSchema(schema string) string {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return "main"
	}
	return schema
}

func (s *Source) fail(err error) {
	if err == nil {
		err = ErrSourceClosed
	}
	s.mu.Lock()
	if s.state == config.SourceStateStopped || s.stopping {
		s.mu.Unlock()
		return
	}
	if s.state == config.SourceStateFaulted {
		s.mu.Unlock()
		return
	}
	s.state = config.SourceStateFaulted
	s.sourceErr = err
	stream := s.observer
	s.observer = nil
	s.observerSource = nil
	snapshotSubscriptions := make([]*subscription, 0, len(s.snapshotSubs))
	snapshotSubs := make([]sqlapi.MutationStream, 0, len(s.snapshotSubs))
	snapshotCancels := s.snapshotAcquisitionCancelsLocked()
	for sub, snapshotStream := range s.snapshotSubs {
		snapshotSubscriptions = append(snapshotSubscriptions, sub)
		snapshotSubs = append(snapshotSubs, snapshotStream)
		delete(s.snapshotSubs, sub)
	}
	s.closeStatusLocked()
	s.mu.Unlock()

	s.subs.closeWithError(err)
	for _, cancel := range snapshotCancels {
		cancel()
	}
	for _, sub := range snapshotSubscriptions {
		sub.closeWithError(err)
		sub.waitRelay()
	}
	for _, snapshotStream := range snapshotSubs {
		_ = snapshotStream.Close()
	}
	if stream != nil {
		_ = stream.Close()
	}
	s.log.Error("sqlite cdc source faulted", zap.String("source", s.name), zap.Error(err))
}

func (s *Source) closeStatusLocked() {
	if s.status != nil && !s.statusClosed {
		close(s.status)
		s.statusClosed = true
	}
}

// Stop closes the observer stream and all subscriptions. It never closes the
// DB observer itself: that belongs to the SQL resource generation.
func (s *Source) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// Serialize cleanup attempts. A timed-out attempt leaves the source in its
	// pre-stop state with stopping set, so a later caller must be able to retry
	// the same cleanup without racing the first attempt or observing a false
	// successful stop.
	if err := s.acquireStop(ctx); err != nil {
		return err
	}
	defer s.releaseStop()

	s.mu.Lock()
	if s.state == config.SourceStateStopped && !s.stopping {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	startCancel := s.startCancel
	runCancel := s.runCancel
	startDone := s.startDone
	runDone := s.runDone
	stream := s.observer
	snapshotStreams := make([]sqlapi.MutationStream, 0, len(s.snapshotSubs))
	snapshotSubscriptions := make([]*subscription, 0, len(s.snapshotSubs))
	snapshotCancels := s.snapshotAcquisitionCancelsLocked()
	for sub, snapshotStream := range s.snapshotSubs {
		snapshotSubscriptions = append(snapshotSubscriptions, sub)
		snapshotStreams = append(snapshotStreams, snapshotStream)
		delete(s.snapshotSubs, sub)
	}
	s.mu.Unlock()

	if startCancel != nil {
		startCancel()
	}
	if runCancel != nil {
		runCancel()
	}
	if stream != nil {
		_ = stream.Close()
	}
	for _, cancel := range snapshotCancels {
		cancel()
	}
	for _, sub := range snapshotSubscriptions {
		sub.closeWithError(nil)
		sub.waitRelay()
	}
	for _, snapshotStream := range snapshotStreams {
		_ = snapshotStream.Close()
	}

	s.mu.Lock()
	snapshotDone := s.snapshotWait
	if snapshotDone == nil {
		snapshotDone = make(chan struct{})
		s.snapshotWait = snapshotDone
		go func() {
			s.snapshotWG.Wait()
			close(snapshotDone)
		}()
	}
	s.mu.Unlock()

	stopTimeout := cleanupTimeout
	if s.lifecycle.StopTimeout > 0 {
		stopTimeout = s.lifecycle.StopTimeout
	}
	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stopTimeout)
	defer cancel()
	if err := waitDone(waitCtx, startDone); err != nil {
		return s.stopFailed(err)
	}
	if err := waitDone(waitCtx, runDone); err != nil {
		return s.stopFailed(err)
	}
	if err := waitDone(waitCtx, snapshotDone); err != nil {
		return s.stopFailed(err)
	}

	s.subs.closeAll()
	s.mu.Lock()
	s.state = config.SourceStateStopped
	s.stopping = false
	s.observer = nil
	s.observerSource = nil
	s.startCancel = nil
	s.runCancel = nil
	s.snapshotWait = nil
	s.closeStatusLocked()
	s.mu.Unlock()
	return nil
}

func (s *Source) acquireStop(ctx context.Context) error {
	if s.stopGate == nil {
		return nil
	}
	select {
	case <-s.stopGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Source) releaseStop() {
	if s.stopGate != nil {
		s.stopGate <- struct{}{}
	}
}

func (s *Source) stopFailed(err error) error {
	s.subs.closeWithError(err)
	s.mu.Lock()
	if s.state != config.SourceStateStopped {
		s.state = config.SourceStateFaulted
		s.sourceErr = err
	}
	s.mu.Unlock()
	return err
}

func waitDone(ctx context.Context, done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Source) beginSnapshotAcquisition(ctx context.Context, observer sqlapi.CommittedMutationSource) (context.Context, uint64, error) {
	acquisitionCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.state != config.SourceStateRunning || s.stopping || s.observerSource != observer {
		s.mu.Unlock()
		cancel()
		return nil, 0, config.ErrSourceNotReady
	}
	s.nextSnapshotID++
	id := s.nextSnapshotID
	s.snapshotAcq[id] = &snapshotAcquisition{cancel: cancel}
	s.snapshotWG.Add(1)
	s.mu.Unlock()
	return acquisitionCtx, id, nil
}

func (s *Source) finishSnapshotAcquisition(id uint64) {
	s.mu.Lock()
	acquisition, ok := s.snapshotAcq[id]
	if ok {
		delete(s.snapshotAcq, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	acquisition.cancel()
	s.snapshotWG.Done()
}

func (s *Source) snapshotAcquisitionCancelsLocked() []context.CancelFunc {
	cancels := make([]context.CancelFunc, 0, len(s.snapshotAcq))
	for _, acquisition := range s.snapshotAcq {
		cancels = append(cancels, acquisition.cancel)
	}
	return cancels
}

// Subscribe exposes committed changes. Cursor resume remains unsupported
// because this process-local generation has no durable checkpoint; snapshots
// use the SQL-owned atomic fence and handoff stream per subscriber.
func (s *Source) Subscribe(ctx context.Context, opts config.StreamOptions) (config.Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	if opts.After != "" {
		return nil, config.ErrUnsupported
	}

	s.mu.RLock()
	if s.state != config.SourceStateRunning || s.stopping {
		s.mu.RUnlock()
		return nil, config.ErrSourceNotReady
	}
	if opts.Snapshot || s.snapshot {
		observer := s.observerSource
		s.mu.RUnlock()
		return s.subscribeSnapshot(ctx, observer, opts)
	}
	// Hold the source read lock while registering the subscription. Stop takes
	// the write lock before closing the current subscriber set, so a stream
	// cannot be inserted after lifecycle cleanup has already passed.
	sub := s.subs.subscribe(s.name, opts)
	s.mu.RUnlock()
	return sub, nil
}

func (s *Source) subscribeSnapshot(ctx context.Context, observer sqlapi.CommittedMutationSource, opts config.StreamOptions) (config.Stream, error) {
	if observer == nil {
		return nil, fmt.Errorf("%w: sqlite snapshot observer is unavailable", config.ErrUnsupported)
	}
	tables, noTables := intersectTables(s.tables, opts.Tables)
	buffer := opts.Buffer
	if buffer <= 0 {
		buffer = defaultStreamBuffer
	}
	if buffer > maxStreamBuffer {
		buffer = maxStreamBuffer
	}
	if noTables {
		// An empty table intersection means no rows can match. Passing an empty
		// list to the SQL observer would mean "all tables", so return an
		// already-complete stream instead.
		sub := newSubscription(s.name, opts, buffer)
		sub.Close()
		return sub, nil
	}

	acquisitionCtx, acquisitionID, err := s.beginSnapshotAcquisition(ctx, observer)
	if err != nil {
		return nil, err
	}
	stream, err := observer.Snapshot(acquisitionCtx, sqlapi.SnapshotOptions{
		Tables: tables,
	})
	if err != nil {
		s.finishSnapshotAcquisition(acquisitionID)
		return nil, err
	}
	if stream == nil {
		s.finishSnapshotAcquisition(acquisitionID)
		return nil, errors.New("sqlite snapshot observer returned a nil stream")
	}

	sub := newSubscription(s.name, opts, buffer)

	s.mu.Lock()
	if s.state != config.SourceStateRunning || s.stopping || s.observerSource != observer {
		s.mu.Unlock()
		_ = stream.Close()
		s.finishSnapshotAcquisition(acquisitionID)
		return nil, config.ErrSourceNotReady
	}
	if _, ok := s.snapshotAcq[acquisitionID]; !ok {
		s.mu.Unlock()
		_ = stream.Close()
		s.finishSnapshotAcquisition(acquisitionID)
		return nil, config.ErrSourceNotReady
	}
	s.snapshotSubs[sub] = stream
	s.snapshotWG.Add(1)
	s.mu.Unlock()

	go s.runSnapshot(acquisitionCtx, stream, sub, acquisitionID)
	return sub, nil
}

func (s *Source) runSnapshot(ctx context.Context, stream sqlapi.SnapshotStream, sub *subscription, acquisitionID uint64) {
	defer s.snapshotWG.Done()
	defer sub.waitRelay()
	defer func() {
		s.mu.Lock()
		delete(s.snapshotSubs, sub)
		s.mu.Unlock()
	}()
	defer s.finishSnapshotAcquisition(acquisitionID)
	// The SQL observer owns the snapshot read transaction and scan worker. A
	// subscriber can end for any reason (upstream error, downstream overflow,
	// cancellation, or normal close), so every return path must release that
	// upstream stream. Close is idempotent for the SQL observer stream.
	defer func() { _ = stream.Close() }()

	watermark := stream.Watermark()
	for {
		select {
		case <-ctx.Done():
			sub.closeWithError(ctx.Err())
			return
		case <-sub.done:
			return
		case batch, ok := <-stream.Changes():
			if !ok {
				err := stream.Err()
				sub.closeWithError(err)
				return
			}
			for i, mutation := range batch.Changes {
				change, err := s.changeFromMutation(batch, i, mutation)
				if err != nil {
					sub.closeWithError(err)
					return
				}
				change.Cursor = snapshotCursor(watermark, batch.Transaction, i)
				if batch.Snapshot {
					change.Op = "snapshot"
				}
				if batch.Snapshot {
					if !sub.matchesSnapshot(change) {
						continue
					}
				} else if !sub.matches(change) {
					continue
				}
				sub.send(change)
				if sub.isClosed() {
					return
				}
			}
		}
	}
}

func snapshotCursor(watermark, transaction string, index int) string {
	parts := make([]string, 0, 3)
	if watermark != "" {
		parts = append(parts, watermark)
	}
	if transaction != "" {
		parts = append(parts, transaction)
	}
	parts = append(parts, strconv.Itoa(index))
	return strings.Join(parts, "/")
}

func intersectTables(source, requested []string) ([]string, bool) {
	if len(source) == 0 {
		return append([]string(nil), requested...), false
	}
	if len(requested) == 0 {
		return append([]string(nil), source...), false
	}
	matched := make([]string, 0, len(requested))
	for _, want := range requested {
		for _, allowed := range source {
			if tableNamesEqual(want, allowed) {
				matched = append(matched, want)
				break
			}
		}
	}
	return matched, len(matched) == 0
}

func tableNamesEqual(left, right string) bool {
	left = strings.ToLower(strings.TrimSpace(left))
	right = strings.ToLower(strings.TrimSpace(right))
	if left == right {
		return true
	}
	left = tableNameOnly(left)
	right = tableNameOnly(right)
	return left != "" && left == right
}

func tableNameOnly(value string) string {
	if index := strings.LastIndexByte(value, '.'); index >= 0 {
		return value[index+1:]
	}
	return value
}

var _ config.Source = (*Source)(nil)
var _ interface {
	Start(context.Context) (<-chan any, error)
	Stop(context.Context) error
} = (*Source)(nil)
