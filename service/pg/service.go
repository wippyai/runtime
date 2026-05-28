// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/health"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// monitorEntry tracks a single group monitor subscription.
type monitorEntry struct {
	pid   pid.PID
	topic string
	id    uint64 // unique ID for unsubscribe
}

// action represents a serialized operation submitted to the event loop.
type action func()

// doneChanPool reuses buffered (cap 1) error channels across Join/Leave/
// JoinGroups/LeaveGroups calls. Each caller acquires a channel, hands it
// to the event-loop closure, and releases it only on the success or
// submit-failure path. On the ctx-cancelled path the channel may still be
// written to later by the (now orphaned) closure, so it is NOT returned
// to the pool — the GC reclaims it when the closure also stops
// referencing it. That makes ctx cancellation a bounded leak, which is
// acceptable since Stop is rare.
var doneChanPool = sync.Pool{
	New: func() any { return make(chan error, 1) },
}

func acquireDoneChan() chan error {
	ch := doneChanPool.Get().(chan error)
	// Drain any stale data so a misuse elsewhere can't poison the pool.
	select {
	case <-ch:
	default:
	}
	return ch
}

func releaseDoneChan(ch chan error) {
	doneChanPool.Put(ch)
}

// serviceCtx bundles the Start()-scoped context and its cancel so they can
// be swapped atomically across Stop/Start cycles without racing concurrent
// submitters reading s.currentCtx().
type serviceCtx struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// livenessActivityCeiling is the maximum time since the last PG broadcast
// (TX or RX) at which the activity-based liveness check still reports
// healthy.
//
// Sized for a sustained-chaos environment where network-partition runs
// continuously and a given pod can be cut off from a randomly-rotating
// 50% subset of peers for tens of seconds at a time. The previous 30s
// ceiling assumed a "transient partition + recovery" model, but under
// continuous random-partition chaos that ceiling tripped /livez 503
// regularly, kubelet then cycled the pod, and the cluster cascaded.
// The lower-level isolation checks (cluster.gossip, raft.last_contact
// per role) already detect a truly disconnected node; this check
// remains as a safety net that catches the case where the pg
// subsystem itself stalls (event loop wedged, all peers gone, retry
// queue saturated) — not a momentary chaos-induced partition.
const livenessActivityCeiling = 5 * time.Minute

// initialDiscoverFanOut caps the number of peers a starting node directly
// discovers. Without this cap, simultaneous restarts of N pods produce N²
// discover messages on a tight startup window. Peers we skip will discover
// us when their memberlist gossip surfaces our NodeJoined.
const initialDiscoverFanOut = 4

// closedServiceCtx is the sentinel returned before Start or after Stop. Its
// context is already cancelled so `<-s.currentCtx().Done()` is non-blocking,
// and submit() rejects new work with ErrServiceStopped — matching the
// "service not running" contract without requiring a nil check at every
// call site.
var closedServiceCtx = func() *serviceCtx {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return &serviceCtx{ctx: ctx, cancel: cancel}
}()

// Service implements the pg (process groups) service.
// All state mutations are serialized through a single goroutine event loop,
// following the Erlang gen_server pattern.
//
// Each Service instance represents an independent PG scope (like Erlang's
// pg:start_link(ScopeName)). Scopes have separate state, event loops,
// and cluster mesh — providing complete isolation.
type Service struct {
	router              relay.Receiver
	topo                topology.Topology
	membership          cluster.Membership
	bus                 event.Bus
	ctxHolder           atomic.Pointer[serviceCtx]
	groupSnaps          sync.Map // group name -> *groupSnapshot, lock-free per-group RCU
	cbManager           *circuitBreakerManager
	nodeJoinedSub       *eventbus.Subscriber
	nodeLeftSub         *eventbus.Subscriber
	actions             chan action
	monitors            map[string][]*monitorEntry
	monitorPIDCounts    map[string]int // pid.String() -> count of monitor subscriptions
	state               *state
	retryQueue          *retryQueue
	tel                 *telemetry
	logger              *zap.Logger
	activity            *activityTracker
	localNodeID         pid.NodeID
	hostID              pid.HostID
	wg                  sync.WaitGroup
	monitorIDSeq        uint64
	maxGroups           int
	maxMembersPerGroup  int
	actionQueueMaxSize  int
	queueWarnThreshold  int
	protocolTimeout     time.Duration
	broadcastTimeout    time.Duration
	antiEntropyInterval time.Duration
	maxRetries          int

	// antiEntropyCursor round-robins anti-entropy syncs across membership
	// peers, one per tick. Touched only from the event loop / reconcile
	// goroutine. Plain int because membership peer ordering is non-stable
	// anyway; modulo over the live set keeps it bounded.
	antiEntropyCursor int

	// queueStress is the current pressure level of the action queue,
	// updated atomically inside submit(). 0=normal, 1=approaching cap,
	// 2=full. submit() logs only on transitions between levels, not on
	// every dropped operation under backpressure (thousands per second
	// under chaos); pg_queue_dropped_total captures the rate as a metric.
	queueStress atomic.Int32
}

// queueStress level constants.
const (
	queueStressNormal      int32 = 0
	queueStressApproaching int32 = 1
	queueStressFull        int32 = 2
)

// LastBroadcastSince returns how long ago the last PG broadcast was
// sent or received by this service. Used by the runtime /livez handler
// to distinguish a partitioned-but-alive pod from one making progress.
func (s *Service) LastBroadcastSince() time.Duration {
	if s == nil || s.activity == nil {
		return 0
	}
	return s.activity.Since()
}

// currentCtx returns the ctx for the active Start() cycle. Before Start or
// after Stop, returns an already-cancelled sentinel so waiters and select
// statements reading `<-s.currentCtx().Done()` behave correctly without a
// nil check. Cheap: single atomic load.
func (s *Service) currentCtx() context.Context {
	return s.ctxHolder.Load().ctx
}

// NewService creates a new pg service for the given scope.
//
// The hostID identifies this scope in the relay system and must be unique
// per node. It is derived from the registry entry ID by the Manager.
// The config controls operational parameters like action queue sizing.
func NewService(
	logger *zap.Logger,
	hostID pid.HostID,
	config *pgapi.Config,
	router relay.Receiver,
	topo topology.Topology,
	membership cluster.Membership,
	bus event.Bus,
	localNodeID pid.NodeID,
	coll metrics.Collector,
	mp otelmetric.MeterProvider,
	tp trace.TracerProvider,
) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	if config == nil {
		config = &pgapi.Config{}
		config.InitDefaults()
	}

	svc := &Service{
		state:               newState(),
		logger:              logger.Named("pg").Named(hostID),
		hostID:              hostID,
		router:              router,
		topo:                topo,
		membership:          membership,
		bus:                 bus,
		localNodeID:         localNodeID,
		actions:             make(chan action, config.ActionQueueMaxSize),
		monitors:            make(map[string][]*monitorEntry),
		monitorPIDCounts:    make(map[string]int),
		activity:            newActivityTracker(),
		maxGroups:           config.MaxGroups,
		maxMembersPerGroup:  config.MaxMembersPerGroup,
		actionQueueMaxSize:  config.ActionQueueMaxSize,
		queueWarnThreshold:  config.ActionQueueSize,
		protocolTimeout:     config.ProtocolTimeout,
		broadcastTimeout:    config.BroadcastTimeout,
		antiEntropyInterval: config.AntiEntropyInterval,
		maxRetries:          config.MaxRetries,
	}
	// Before Start (or after Stop) reads of s.currentCtx() return an
	// already-cancelled sentinel so submit() rejects work cleanly and no
	// call site needs a nil check.
	svc.ctxHolder.Store(closedServiceCtx)

	svc.tel = newTelemetry(coll, mp, tp)

	// Initialize circuit breaker manager
	svc.cbManager = newCircuitBreakerManager(
		config.CircuitBreakerFailures,
		config.CircuitBreakerResetTime,
		logger,
		svc.tel,
	)

	// Initialize retry queue
	svc.retryQueue = newRetryQueue(
		svc,
		config.MaxRetries,
		config.RetryBaseDelay,
		config.RetryMaxDelay,
		logger,
		svc.tel,
	)

	return svc
}

// HostID returns the relay host ID for this PG scope.
func (s *Service) HostID() pid.HostID {
	return s.hostID
}

// Start begins the pg service event loop and subscribes to cluster events.
// The Start-scoped context is published atomically via ctxHolder so concurrent
// submitters racing against Start/Stop observe a consistent ctx without lock.
func (s *Service) Start(ctx context.Context) (<-chan any, error) {
	runCtx, cancel := context.WithCancel(ctx)
	sctx := &serviceCtx{ctx: runCtx, cancel: cancel}
	s.ctxHolder.Store(sctx)

	statusChan := make(chan any, 1)

	// Subscribe to cluster events for node discovery
	if s.bus != nil {
		var err error
		s.nodeJoinedSub, err = eventbus.NewSubscriber(runCtx, s.bus, cluster.System, cluster.NodeJoined, s.handleNodeJoinedEvent)
		if err != nil {
			cancel()
			s.ctxHolder.Store(closedServiceCtx)
			return nil, err
		}

		s.nodeLeftSub, err = eventbus.NewSubscriber(runCtx, s.bus, cluster.System, cluster.NodeLeft, s.handleNodeLeftEvent)
		if err != nil {
			s.nodeJoinedSub.Close()
			cancel()
			s.ctxHolder.Store(closedServiceCtx)
			return nil, err
		}
	}

	// Per-group snapshots are published lazily as groups gain members.
	// Lock-free readers see an empty result for unknown groups until
	// the first publishDirty for that group fires.

	// Start event loop
	s.wg.Add(1)
	go s.eventLoop()

	// Start periodic anti-entropy reconcile so any join/leave broadcast a
	// membership peer missed eventually converges. Disabled when interval
	// <= 0 or membership is unconfigured (single-node / direct-protocol tests).
	if s.antiEntropyInterval > 0 && s.membership != nil {
		s.wg.Add(1)
		go s.antiEntropyLoop(runCtx)
	}

	// Start retry queue
	s.retryQueue.Start(runCtx)

	// Discover existing nodes. Cap startup fan-out so simultaneous restarts
	// of N pods don't produce N² discover messages — peers we skip will
	// eventually discover us when their memberlist NodeJoined event for our
	// node fires and triggers their own handleNodeJoinedEvent → sendDiscover.
	if s.membership != nil {
		localNode := s.membership.LocalNode()
		peers := make([]pid.NodeID, 0)
		for _, node := range s.membership.Nodes() {
			if node.ID != localNode.ID {
				peers = append(peers, node.ID)
			}
		}
		targets := pickInitialDiscoverTargets(peers, initialDiscoverFanOut)
		s.tel.recordDiscoverTargets("initial", len(targets), len(peers))
		for _, nodeID := range targets {
			s.submit(func() {
				s.sendDiscover(nodeID)
			})
		}
	}

	s.logger.Info("pg service started",
		zap.String("node", s.localNodeID),
		zap.Int("queue_size", cap(s.actions)),
		zap.Int("queue_max", s.actionQueueMaxSize),
	)

	// Register an activity-based liveness check. Reports unhealthy when
	// the service has gone more than livenessActivityCeiling without
	// emitting OR receiving a broadcast — the symptom of being stuck
	// on the minority side of a partition. Threshold > maxBroadcastInterval
	// in chaos_workload.lua + a chaos-recoverable margin.
	health.Register("pg.broadcast_recent."+s.hostID, func() error {
		if since := s.activity.Since(); since > livenessActivityCeiling {
			return fmt.Errorf("no broadcast in %s (ceiling %s)", since.Round(time.Second), livenessActivityCeiling)
		}
		return nil
	})

	select {
	case statusChan <- "pg service started":
	default:
	}

	return statusChan, nil
}

// Stop shuts down the pg service. Safe to call concurrently with Start or
// with submitters: the atomic swap to closedServiceCtx ensures the old
// ctx is cancelled exactly once and subsequent submit() calls observe a
// cancelled context without racing the field write.
func (s *Service) Stop(_ context.Context) error {
	s.logger.Info("pg service stopping")

	// Detach the liveness check so a stopped service does not report
	// 503 forever to /livez. Re-registered on the next Start().
	health.Register("pg.broadcast_recent."+s.hostID, nil)

	if s.nodeJoinedSub != nil {
		s.nodeJoinedSub.Close()
	}
	if s.nodeLeftSub != nil {
		s.nodeLeftSub.Close()
	}

	// Stop retry queue
	if s.retryQueue != nil {
		s.retryQueue.Stop()
	}

	// Swap the live ctx holder out atomically; then cancel the old one so
	// any in-flight select using the previous ctx unblocks. New submitters
	// will now observe the closed sentinel and reject via submitError().
	old := s.ctxHolder.Swap(closedServiceCtx)
	if old != nil && old != closedServiceCtx {
		old.cancel()
	}
	s.wg.Wait()

	// Drop per-group snapshots so post-Stop readers return empty results
	// even before the Service is garbage-collected.
	s.groupSnaps.Range(func(k, _ any) bool {
		s.groupSnaps.Delete(k)
		return true
	})

	s.logger.Info("pg service stopped")
	return nil
}

// eventLoop runs the single-threaded event loop that processes all actions.
// After each action, it publishes an immutable snapshot for lock-free reads.
func (s *Service) eventLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.currentCtx().Done():
			return
		case fn, ok := <-s.actions:
			if !ok {
				return
			}
			fn()
		}
	}
}

// publishDirty publishes a fresh per-group snapshot for every group the
// current event-loop closure touched, then clears the dirty set. Cost is
// O(sum of members across dirty groups) — independent of the total
// number of groups in the scope. Must be called inside the event loop
// (inside an action closure) after state mutation, BEFORE signaling the
// caller's done channel, so lock-free readers see the updated state.
func (s *Service) publishDirty() {
	if len(s.state.dirty) == 0 {
		return
	}
	for group := range s.state.dirty {
		if snap := s.state.snapshotGroup(group); snap != nil {
			s.groupSnaps.Store(group, snap)
		} else {
			s.groupSnaps.Delete(group)
		}
		delete(s.state.dirty, group)
	}
}

// submit sends an action to the event loop for serialized execution.
// Returns true if submitted, false if queue is full (for non-blocking callers).
//
// Logging discipline: pg_queue_depth (gauge) and pg_queue_dropped_total
// (counter, labeled with reason="full"|"cancelled") capture the
// per-operation rate as metrics. We only emit a log line when the
// queue's stress level *transitions* (e.g. normal→approaching→full or
// recovery), so the noise floor stays bounded under sustained chaos.
func (s *Service) submit(fn action) bool {
	depth := len(s.actions)
	s.tel.recordQueueDepth(s.hostID, depth)

	stress := queueStressNormal
	switch {
	case depth >= s.actionQueueMaxSize:
		stress = queueStressFull
	case depth >= s.queueWarnThreshold:
		stress = queueStressApproaching
	}
	s.logQueueStressTransition(stress, depth)

	// Non-blocking send: the channel is sized to the hard cap, so a full
	// channel means genuine saturation. Drop rather than block the caller
	// (including cluster-event handlers that feed this loop). Lossy under
	// extreme backpressure, observable via pg_queue_dropped_total{full}.
	select {
	case s.actions <- fn:
		return true
	default:
		s.tel.recordQueueDropped(s.hostID, "full")
		return false
	}
}

// logQueueStressTransition emits a single log line each time the queue
// crosses a stress threshold. CompareAndSwap ensures concurrent
// submitters racing through the same transition only emit once.
func (s *Service) logQueueStressTransition(next int32, depth int) {
	prev := s.queueStress.Load()
	if prev == next {
		return
	}
	if !s.queueStress.CompareAndSwap(prev, next) {
		return
	}
	switch next {
	case queueStressFull:
		s.logger.Info("action queue full",
			zap.Int("queue_len", depth),
			zap.Int("queue_max", s.actionQueueMaxSize),
		)
	case queueStressApproaching:
		s.logger.Info("action queue approaching capacity",
			zap.Int("queue_len", depth),
			zap.Int("queue_max", s.actionQueueMaxSize),
			zap.Int("threshold", s.queueWarnThreshold),
		)
	case queueStressNormal:
		if prev != queueStressNormal {
			s.logger.Info("action queue back to normal",
				zap.Int("queue_len", depth),
				zap.Int("queue_max", s.actionQueueMaxSize),
			)
		}
	}
}

// submitError returns the appropriate error when submit() returns false:
// ErrServiceStopped if the context is done, ErrBackpressure if the queue is full.
func (s *Service) submitError() error {
	select {
	case <-s.currentCtx().Done():
		return ErrServiceStopped
	default:
		return ErrBackpressure
	}
}

// These methods use the serialized event loop for thread safety.

// --- Logging helpers ---

func logNodeID(nodeID pid.NodeID) zap.Field {
	return zap.String("node_id", nodeID)
}

func logError(err error) zap.Field {
	return zap.Error(err)
}

func logPID(p pid.PID) zap.Field {
	return zap.String("pid", p.String())
}

func logGroupCount(count int) zap.Field {
	return zap.Int("group_count", count)
}

// --- resource.Provider implementation ---

// Acquire returns a resource handle for this PG scope.
// The returned resource's Get() yields the ProcessGroups interface.
func (s *Service) Acquire(_ context.Context, _ registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if mode != resource.ModeNormal {
		return nil, resource.ErrReleased
	}
	// Check if service is running via the running context sentinel; the
	// closed sentinel is installed by Stop and before Start.
	if s.currentCtx().Err() != nil {
		return nil, ErrServiceStopped
	}
	return &pgResource{svc: s}, nil
}

// pgResource wraps a Service as a resource.Resource[any].
type pgResource struct {
	svc    *Service
	closed bool
	mu     sync.Mutex
}

// Get returns the ScopeService interface for this scope.
func (r *pgResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil, resource.ErrReleased
	}

	return pgapi.ScopeService(r.svc), nil
}

// Release frees the resource handle.
func (r *pgResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	r.closed = true
}

// Verify interface compliance.
var _ pgapi.ProcessGroups = (*Service)(nil)
var _ pgapi.ScopeService = (*Service)(nil)
var _ relay.Receiver = (*Service)(nil)
var _ resource.Provider = (*Service)(nil)
var _ supervisor.Service = (*Service)(nil)
