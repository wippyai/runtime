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
	"github.com/wippyai/runtime/api/payload"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/health"
	"go.opentelemetry.io/otel/attribute"
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
	// 2=full. The hot-path Warn at submit() used to fire on every
	// dropped operation under backpressure (thousands per second under
	// chaos). We now log only on transitions between levels — the
	// pg_queue_dropped_total counter captures the rate as a metric.
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

// Send implements relay.Receiver for the pg host.
// Incoming relay packages are dispatched to protocol handlers.
func (s *Service) Send(pkg *relay.Package) error {
	if pkg == nil || len(pkg.Messages) == 0 {
		return nil
	}

	// Any inbound PG protocol package counts as activity for the
	// liveness signal — receiving join/leave/sync from a peer is
	// progress, even if no local broadcast was emitted.
	s.activity.Touch()

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case pgapi.TopicDiscover:
			s.handleDiscoverPackage(msg)
		case pgapi.TopicSync:
			s.handleSyncPackage(msg)
		case pgapi.TopicJoin:
			s.handleJoinPackage(msg)
		case pgapi.TopicLeave:
			s.handleLeavePackage(msg)
		case topology.TopicEvents:
			s.handleExitPackage(msg)
		}
	}

	relay.ReleasePackage(pkg)
	return nil
}

// handleDiscoverPackage processes an incoming discover message.
func (s *Service) handleDiscoverPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	// pg_queue_dropped_total{reason="full"} is recorded inside submit()
	// when the queue rejects the operation; no per-message log needed.
	s.submit(func() {
		s.handleDiscover(fromNodeID)
		s.publishDirty()
	})
}

// handleSyncPackage processes an incoming sync message.
func (s *Service) handleSyncPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}
	rawGroups, _ := data["groups"].(map[string]any)

	groups := make(map[string][]pid.PID, len(rawGroups))
	for group, raw := range rawGroups {
		pidStrs, ok := raw.([]any)
		if !ok {
			continue
		}
		pids := make([]pid.PID, 0, len(pidStrs))
		for _, ps := range pidStrs {
			if s, ok := ps.(string); ok {
				if p, err := pid.ParsePID(s); err == nil {
					pids = append(pids, p)
				}
			}
		}
		if len(pids) > 0 {
			groups[group] = pids
		}
	}

	s.submit(func() {
		s.handleSync(fromNodeID, groups)
		s.publishDirty()
	})
}

// decodeGroupPidsMap parses a `map[string][]string` payload field (the
// receiver-side decoded shape after relay serialization) into the
// internal map[string][]pid.PID form, skipping unparseable entries.
func decodeGroupPidsMap(raw any) map[string][]pid.PID {
	rawMap, ok := raw.(map[string]any)
	if !ok || len(rawMap) == 0 {
		return nil
	}
	result := make(map[string][]pid.PID, len(rawMap))
	for g, rawPids := range rawMap {
		pids := decodePidList(rawPids)
		if len(pids) > 0 {
			result[g] = pids
		}
	}
	return result
}

// decodePidList parses a `[]any` of pid strings into []pid.PID.
func decodePidList(raw any) []pid.PID {
	rawSlice, ok := raw.([]any)
	if !ok || len(rawSlice) == 0 {
		return nil
	}
	pids := make([]pid.PID, 0, len(rawSlice))
	for _, ps := range rawSlice {
		s, ok := ps.(string)
		if !ok {
			continue
		}
		if p, err := pid.ParsePID(s); err == nil {
			pids = append(pids, p)
		}
	}
	return pids
}

// handleJoinPackage processes an incoming join message. The payload carries
// a `joins` map of {group -> pid strings}; one packet may cover multiple
// groups (batched broadcast).
func (s *Service) handleJoinPackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	joins := decodeGroupPidsMap(data["joins"])
	if len(joins) == 0 {
		// Fallback to the pre-batch single-group format
		// ({"group": "...", "pids": [...]}) so older senders still work
		// through a rolling upgrade.
		if group, _ := data["group"].(string); group != "" {
			if pids := decodePidList(data["pids"]); len(pids) > 0 {
				joins = map[string][]pid.PID{group: pids}
			}
		}
	}
	if len(joins) == 0 {
		return
	}

	s.submit(func() {
		for group, pids := range joins {
			s.handleRemoteJoin(fromNodeID, group, pids)
		}
		s.publishDirty()
	})
}

// handleLeavePackage processes an incoming leave message. The payload
// carries a `leaves` map of {group -> pid strings}; PIDs repeated in a
// group's value list cause the matching number of multi-join slots to be
// removed.
func (s *Service) handleLeavePackage(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	data, ok := msg.Payloads[0].Data().(map[string]any)
	if !ok {
		return
	}
	fromNodeID, _ := data["from"].(string)
	if fromNodeID == "" {
		return
	}

	leaves := decodeGroupPidsMap(data["leaves"])
	if len(leaves) == 0 {
		// Fallback to the pre-batch flat ({pids, groups}) shape where the
		// receiver must remove each pid from each group once. Translate
		// it into the batched map by repeating each pid per group entry.
		pids := decodePidList(data["pids"])
		rawGroups, _ := data["groups"].([]any)
		if len(pids) > 0 && len(rawGroups) > 0 {
			leaves = make(map[string][]pid.PID, len(rawGroups))
			for _, raw := range rawGroups {
				g, ok := raw.(string)
				if !ok || g == "" {
					continue
				}
				leaves[g] = append(leaves[g], pids...)
			}
		}
	}
	if len(leaves) == 0 {
		return
	}

	s.submit(func() {
		for group, pids := range leaves {
			s.handleRemoteLeave(fromNodeID, pids, []string{group})
		}
		s.publishDirty()
	})
}

// handleExitPackage processes an incoming process exit event.
func (s *Service) handleExitPackage(msg *relay.Message) {
	for _, p := range msg.Payloads {
		if exitEvent, ok := p.Data().(*topology.ExitEvent); ok {
			exitedPID := exitEvent.From
			s.submit(func() {
				s.handleProcessExit(exitedPID)
				s.publishDirty()
			})
		}
	}
}

// handleNodeJoinedEvent is called by the event bus subscriber when a node joins.
func (s *Service) handleNodeJoinedEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.sendDiscover(nodeID)
	})
}

// handleNodeLeftEvent is called by the event bus subscriber when a node leaves.
func (s *Service) handleNodeLeftEvent(e event.Event) {
	nodeEvent, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}
	nodeID := nodeEvent.Node.ID
	if nodeID == s.localNodeID {
		return
	}

	s.submit(func() {
		s.handleNodeLeft(nodeID)
		s.publishDirty()
	})
}

// --- ProcessGroups interface implementation ---
// These methods use the serialized event loop for thread safety.

// Join adds a local process to a group.
func (s *Service) Join(group pgapi.Group, p pid.PID) error {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.join",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	done := acquireDoneChan()
	if !s.submit(func() {
		// Enforce MaxGroups: if the group doesn't exist yet, check limit
		if s.maxGroups > 0 {
			if _, exists := s.state.groups[group]; !exists {
				if len(s.state.groups) >= s.maxGroups {
					done <- pgapi.ErrMaxGroupsReached
					return
				}
			}
		}

		// Enforce MaxMembersPerGroup
		if s.maxMembersPerGroup > 0 {
			if gs, exists := s.state.groups[group]; exists {
				if len(gs.all) >= s.maxMembersPerGroup {
					done <- pgapi.ErrMaxMembersReached
					return
				}
			}
		}

		// Check if this is the first join for this process
		_, existed := s.state.local[p.String()]
		s.state.joinLocal(group, p)

		// Monitor the process if this is the first join
		if !existed {
			s.monitorProcess(p)
		}

		// Broadcast to remote nodes
		s.broadcastJoin(map[string][]pid.PID{group: {p}})

		// Emit membership event
		s.emitJoinEvent(group, []pid.PID{p})

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordJoin(group, err, time.Since(start))
		return err
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		s.tel.setSpanError(span, err)
		s.tel.recordJoin(group, err, time.Since(start))
		return err
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordJoin(group, ErrServiceStopped, time.Since(start))
		return ErrServiceStopped
	}
}

// JoinGroups adds a local process to multiple groups atomically.
func (s *Service) JoinGroups(groups []pgapi.Group, p pid.PID) error {
	done := acquireDoneChan()
	if !s.submit(func() {
		// Pre-check all limits before mutating state (atomic: all-or-nothing)
		if s.maxGroups > 0 || s.maxMembersPerGroup > 0 {
			newGroups := make(map[pgapi.Group]struct{}, len(groups))
			projectedMembers := make(map[pgapi.Group]int, len(groups))
			for _, group := range groups {
				if _, tracked := projectedMembers[group]; !tracked {
					projectedMembers[group] = 0
					if gs, exists := s.state.groups[group]; exists {
						projectedMembers[group] = len(gs.all)
					}
				}

				if s.maxGroups > 0 {
					if _, exists := s.state.groups[group]; !exists {
						newGroups[group] = struct{}{}
					}
				}
				if s.maxMembersPerGroup > 0 {
					projectedMembers[group]++
					if projectedMembers[group] > s.maxMembersPerGroup {
						done <- pgapi.ErrMaxMembersReached
						return
					}
				}
			}
			if s.maxGroups > 0 && len(s.state.groups)+len(newGroups) > s.maxGroups {
				done <- pgapi.ErrMaxGroupsReached
				return
			}
		}

		_, existed := s.state.local[p.String()]

		joins := make(map[string][]pid.PID, len(groups))
		for _, group := range groups {
			s.state.joinLocal(group, p)
			joins[group] = append(joins[group], p)
			s.emitJoinEvent(group, []pid.PID{p})
		}
		s.broadcastJoin(joins)

		if !existed {
			s.monitorProcess(p)
		}

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		return s.submitError()
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		return err
	case <-s.currentCtx().Done():
		return ErrServiceStopped
	}
}

// Leave removes a local process from a group.
func (s *Service) Leave(group pgapi.Group, p pid.PID) error {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.leave",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	done := acquireDoneChan()
	if !s.submit(func() {
		if !s.state.leaveLocal(group, p) {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		// Broadcast to remote nodes
		s.broadcastLeave(map[string][]pid.PID{group: {p}})

		// Emit membership event
		s.emitLeaveEvent(group, []pid.PID{p})

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordLeave(group, err, time.Since(start))
		return err
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		s.tel.setSpanError(span, err)
		s.tel.recordLeave(group, err, time.Since(start))
		return err
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordLeave(group, ErrServiceStopped, time.Since(start))
		return ErrServiceStopped
	}
}

// LeaveGroups removes a local process from multiple groups.
// Following Erlang PG semantics: leaves all groups where the process is a member,
// skips groups where it isn't, and returns ErrNotJoined only if the process
// was not a member of ANY of the specified groups.
func (s *Service) LeaveGroups(groups []pgapi.Group, p pid.PID) error {
	done := acquireDoneChan()
	if !s.submit(func() {
		anyLeft := false
		leaves := make(map[string][]pid.PID, len(groups))
		for _, group := range groups {
			if s.state.leaveLocal(group, p) {
				anyLeft = true
				leaves[group] = append(leaves[group], p)
				s.emitLeaveEvent(group, []pid.PID{p})
			}
		}
		if anyLeft {
			s.broadcastLeave(leaves)
		}

		if !anyLeft {
			done <- ErrNotJoined
			return
		}

		// If the process has no more groups, stop monitoring
		if _, exists := s.state.local[p.String()]; !exists {
			s.demonitorProcess(p)
		}

		s.publishDirty()
		done <- nil
	}) {
		releaseDoneChan(done)
		return s.submitError()
	}

	select {
	case err := <-done:
		releaseDoneChan(done)
		return err
	case <-s.currentCtx().Done():
		return ErrServiceStopped
	}
}

// loadGroupSnap returns the immutable snapshot for a single group, or nil
// if the group is absent. O(1) amortized.
func (s *Service) loadGroupSnap(group pgapi.Group) *groupSnapshot {
	v, ok := s.groupSnaps.Load(group)
	if !ok {
		return nil
	}
	return v.(*groupSnapshot)
}

// GetMembers returns all members of a group across all nodes.
// Lock-free O(M_g) where M_g is the number of members in this group.
func (s *Service) GetMembers(group pgapi.Group) []pid.PID {
	gs := s.loadGroupSnap(group)
	if gs == nil {
		return nil
	}
	return copyPIDs(gs.all)
}

// GetLocalMembers returns local members of a group.
// Lock-free O(M_g_local) where M_g_local is the number of local members.
func (s *Service) GetLocalMembers(group pgapi.Group) []pid.PID {
	gs := s.loadGroupSnap(group)
	if gs == nil {
		return nil
	}
	return copyPIDs(gs.local)
}

// WhichGroups returns all groups that have at least one member.
// O(N) iteration over the per-group snapshot map; intended for
// discovery/debugging, not hot paths.
func (s *Service) WhichGroups() []pgapi.Group {
	var groups []pgapi.Group
	s.groupSnaps.Range(func(k, _ any) bool {
		groups = append(groups, k.(pgapi.Group))
		return true
	})
	return groups
}

// WhichLocalGroups returns groups that have at least one local member.
// O(N) iteration; cold path.
func (s *Service) WhichLocalGroups() []pgapi.Group {
	var groups []pgapi.Group
	s.groupSnaps.Range(func(k, v any) bool {
		if gs := v.(*groupSnapshot); len(gs.local) > 0 {
			groups = append(groups, k.(pgapi.Group))
		}
		return true
	})
	return groups
}

// Monitor atomically subscribes to a group's membership events and returns
// the current members. Because both operations happen inside a single event
// loop action, no join/leave can interleave between the subscription setup
// and the membership snapshot.
func (s *Service) Monitor(group string, p pid.PID, topic string) pgapi.MonitorResult {
	done := make(chan pgapi.MonitorResult, 1)
	if !s.submit(func() {
		// Assign unique ID
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		s.monitors[group] = append(s.monitors[group], entry)
		s.monitorPIDCounts[p.String()]++

		// Monitor the subscriber process so we can clean up if it dies.
		// monitorProcess is idempotent (ignores ErrAlreadyMonitoring).
		s.monitorProcess(p)

		// Snapshot current members (while still in event loop — atomic)
		members := s.state.getMembers(group)

		// Build unsubscribe closure — synchronous: blocks until the event
		// loop processes the removal, so no more events can be emitted after
		// unsubscribe returns (matches Erlang pg:demonitor/2 semantics).
		unsubscribe := func() {
			unsub := make(chan struct{}, 1)
			s.submit(func() {
				s.removeMonitor(group, id, p)
				// If the process has no more group memberships and no more
				// monitor subscriptions, stop monitoring it.
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			})
			select {
			case <-unsub:
			case <-s.currentCtx().Done():
			}
		}

		done <- pgapi.MonitorResult{Members: members, Unsubscribe: unsubscribe}
	}) {
		return pgapi.MonitorResult{}
	}

	select {
	case result := <-done:
		return result
	case <-s.currentCtx().Done():
		return pgapi.MonitorResult{}
	}
}

// removeMonitor removes a monitor entry by ID and decrements the PID count.
// Must be called from event loop.
func (s *Service) removeMonitor(group string, id uint64, p pid.PID) {
	entries := s.monitors[group]
	for i, e := range entries {
		if e.id == id {
			s.monitors[group] = append(entries[:i], entries[i+1:]...)
			key := p.String()
			if s.monitorPIDCounts[key] > 0 {
				s.monitorPIDCounts[key]--
				if s.monitorPIDCounts[key] == 0 {
					delete(s.monitorPIDCounts, key)
				}
			}
			break
		}
	}
	if len(s.monitors[group]) == 0 {
		delete(s.monitors, group)
	}
}

// removeMonitorsByPID removes all monitor subscriptions owned by the given PID.
// This mirrors Erlang PG's automatic cleanup when a monitoring process dies.
// Must be called from the event loop.
func (s *Service) removeMonitorsByPID(p pid.PID) {
	key := p.String()
	for group, entries := range s.monitors {
		var remaining []*monitorEntry
		for _, e := range entries {
			if e.pid.String() != key {
				remaining = append(remaining, e)
			}
		}
		if len(remaining) == 0 {
			delete(s.monitors, group)
		} else if len(remaining) != len(entries) {
			s.monitors[group] = remaining
		}
	}
	delete(s.monitorPIDCounts, key)
}

// removeMonitorsByNode removes all monitor subscriptions owned by PIDs
// hosted on the departed node. Without this, monitor entries leak forever
// for any node that left without each of its PIDs explicitly demonitoring
// (the common case under partition / pod kill chaos). The PID-level
// cleanup in removeMonitorsByPID does not cover this because it requires
// knowing every owning PID; the node-level cleanup is the only one that
// can be triggered on the cluster.NodeLeft event alone.
//
// Returns the number of entries evicted, for telemetry.
// Must be called from the event loop.
func (s *Service) removeMonitorsByNode(nodeID pid.NodeID) int {
	if nodeID == "" {
		return 0
	}
	evicted := 0
	for group, entries := range s.monitors {
		var remaining []*monitorEntry
		for _, e := range entries {
			if e.pid.Node != nodeID {
				remaining = append(remaining, e)
				continue
			}
			evicted++
			key := e.pid.String()
			if s.monitorPIDCounts[key] > 0 {
				s.monitorPIDCounts[key]--
				if s.monitorPIDCounts[key] == 0 {
					delete(s.monitorPIDCounts, key)
				}
			}
		}
		if len(remaining) == 0 {
			delete(s.monitors, group)
		} else if len(remaining) != len(entries) {
			s.monitors[group] = remaining
		}
	}
	return evicted
}

// hasMonitorSubscriptions returns true if the given PID has any active
// monitor subscriptions (group-specific or wildcard). O(1) via reverse index.
// Must be called from the event loop.
func (s *Service) hasMonitorSubscriptions(p pid.PID) bool {
	return s.monitorPIDCounts[p.String()] > 0
}

// hasGroupMemberships returns true if the given PID is a member of any
// local group. Must be called from the event loop.
func (s *Service) hasGroupMemberships(p pid.PID) bool {
	_, exists := s.state.local[p.String()]
	return exists
}

// Events atomically subscribes to all group membership events and returns
// a snapshot of all current groups and their members. Uses the wildcard
// monitor key ("") to receive events for all groups.
func (s *Service) Events(p pid.PID, topic string) pgapi.EventsResult {
	done := make(chan pgapi.EventsResult, 1)
	if !s.submit(func() {
		s.monitorIDSeq++
		id := s.monitorIDSeq

		entry := &monitorEntry{
			pid:   p,
			topic: topic,
			id:    id,
		}

		// Wildcard key "" matches all groups
		s.monitors[""] = append(s.monitors[""], entry)
		s.monitorPIDCounts[p.String()]++

		// Monitor the subscriber process so we can clean up if it dies.
		s.monitorProcess(p)

		// Snapshot all current groups
		groups := s.state.allGroupMembers()

		unsubscribe := func() {
			unsub := make(chan struct{}, 1)
			s.submit(func() {
				s.removeMonitor("", id, p)
				if !s.hasMonitorSubscriptions(p) && !s.hasGroupMemberships(p) {
					s.demonitorProcess(p)
				}
				unsub <- struct{}{}
			})
			select {
			case <-unsub:
			case <-s.currentCtx().Done():
			}
		}

		done <- pgapi.EventsResult{Groups: groups, Unsubscribe: unsubscribe}
	}) {
		return pgapi.EventsResult{}
	}

	select {
	case result := <-done:
		return result
	case <-s.currentCtx().Done():
		return pgapi.EventsResult{}
	}
}

// Broadcast sends a message to all members of a group across all nodes.
func (s *Service) Broadcast(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	span := noopSpan
	if s.tel.tracing {
		_, span = s.tel.tracer.Start(s.currentCtx(), "pg.broadcast",
			trace.WithAttributes(
				attribute.String("pg.name", group),
				attribute.String("node.id", s.localNodeID),
			),
		)
		defer span.End()
	}

	start := time.Now()
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	if !s.submit(func() {
		membersCh <- s.state.getMembers(group)
	}) {
		err := s.submitError()
		s.tel.setSpanError(span, err)
		s.tel.recordBroadcast(group, 0, err, time.Since(start))
		return 0, err
	}

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.currentCtx().Done():
		s.tel.setSpanError(span, ErrServiceStopped)
		s.tel.recordBroadcast(group, 0, ErrServiceStopped, time.Since(start))
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	span.SetAttributes(attribute.Int("pg.recipients", sent))
	s.tel.recordBroadcast(group, sent, nil, time.Since(start))
	s.activity.Touch()
	return sent, nil
}

// BroadcastLocal sends a message to local members of a group only.
func (s *Service) BroadcastLocal(from pid.PID, group pgapi.Group, topic string, payloads payload.Payloads) (int, error) {
	// Snapshot members inside the event loop for consistency.
	membersCh := make(chan []pid.PID, 1)
	if !s.submit(func() {
		membersCh <- s.state.getLocalMembers(group)
	}) {
		return 0, s.submitError()
	}

	var members []pid.PID
	select {
	case members = <-membersCh:
	case <-s.currentCtx().Done():
		return 0, ErrServiceStopped
	}

	// Send outside the event loop so we don't block the action queue.
	sent := s.sendToMembers(from, topic, payloads, members)
	return sent, nil
}

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

// --- Event emission ---

// emitJoinEvent delivers a membership join event to group monitors via the relay.
func (s *Service) emitJoinEvent(group string, pids []pid.PID) {
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberJoined, pids)
}

// emitLeaveEvent delivers a membership leave event to group monitors via the relay.
func (s *Service) emitLeaveEvent(group string, pids []pid.PID) {
	s.deliverMonitorEventWithCircuitBreaker(group, pgapi.MemberLeft, pids)
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
