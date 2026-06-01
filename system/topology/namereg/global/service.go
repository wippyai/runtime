// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/api/topology/namereg/global"

	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// HostID is the relay host ID for the global registry service.
	HostID pid.HostID = "globalreg"

	// Topics for inter-node leader forwarding.
	topicForwardRequest  relay.Topic = "global.forward.request"
	topicForwardResponse relay.Topic = "global.forward.response"
	// topicRegisterAck delivers a Strong-scope ack from a follower to the
	// current leader. Body is a msgpack-encoded ackEnvelope. The wire string
	// retains "root" as the on-the-wire identifier for the Strong scope.
	topicRegisterAck relay.Topic = "global.root.ack"
	// topicCheckPending is an out-of-band relay nudge the leader sends to a
	// missing required node, asking it to re-evaluate the pending reservation
	// and re-emit its ack. Body is a msgpack-encoded checkPendingEnvelope.
	// The nudge never mutates Raft state — only ACK/REJECT/DROP/EXPIRED do.
	topicCheckPending relay.Topic = "global.root.check"
	// topicReleaseExclusion is a targeted relay message the leader sends to a
	// node holding a Strong exclusion when the name reaches a terminal state
	// (expire/reject/unregister, including an active name being unregistered).
	// Body is a msgpack-encoded releaseEnvelope; the recipient releases its
	// exclusion for the carried (name, epoch) idempotently. Never mutates Raft.
	topicReleaseExclusion relay.Topic = "global.root.release"
	// topicJoinRequest carries a node's JoinNameEpoch request to the leader. Body
	// is a msgpack-encoded joinRequestEnvelope. The leader replies on
	// topicJoinResponse with a snapshot of PENDING∪ACTIVE Strong names as of the
	// current commit index. Never mutates Raft; the wire string retains "root".
	topicJoinRequest relay.Topic = "global.root.join"
	// topicJoinResponse delivers the leader's join snapshot back to the
	// requesting node. Body is a msgpack-encoded joinResponseEnvelope.
	topicJoinResponse relay.Topic = "global.root.join.resp"
	// topicLeaderPing carries a minimal leader-reachability probe to the leader.
	// Body is a msgpack-encoded leaderPingEnvelope. The leader replies on
	// topicLeaderPong. A successful round-trip attests the leader is reachable
	// without building a snapshot — the cheap counterpart to JoinNameEpoch the
	// reachability monitor polls. Never mutates Raft; the wire string retains
	// "root".
	topicLeaderPing relay.Topic = "global.root.ping"
	// topicLeaderPong delivers the leader's ping reply back to the prober.
	topicLeaderPong relay.Topic = "global.root.pong"

	defaultApplyTimeout = 10 * time.Second

	// strongRebroadcastDivisor controls how often the leader re-broadcasts
	// pending entries. Set to 4 so a 10s deadline triggers ~2 rebroadcasts.
	strongRebroadcastDivisor = 4
)

// Service implements the global.Registry interface.
// It wraps a Raft-backed FSM and provides leader forwarding for writes
// and topology-based auto-cleanup.
type Service struct {
	localPresence    atomic.Value
	router           relay.Receiver
	raftSvc          raftapi.Service
	bus              event.Bus
	topo             topology.Topology
	membership       cluster.Membership
	dissem           atomic.Value
	localRevoker     atomic.Value
	pingPending      map[uint64]chan struct{}
	joinPending      map[uint64]chan *joinResponseEnvelope
	memberDeriver    MemberDeriver
	pending          map[uint64]chan *forwardResponse
	forwardProxies   map[uint64]pid.NodeID
	strongWatchers   map[string]map[uint64]chan strongOutcome
	strongTimers     map[string]*strongTimer
	fsm              *FSM
	logger           *zap.Logger
	stopCh           chan struct{}
	lookupPending    map[uint64]chan *lookupResponseEnvelope
	ackerEpochs      map[pid.NodeID]uint64
	strongExclusions map[string]strongExclusion
	tel              *telemetry
	monitoredPIDs    sync.Map
	localNode        pid.NodeID
	probeInterval    time.Duration
	probeGrace       int
	monitorWatermark atomic.Uint64
	nodeEpoch        atomic.Uint64
	lookupMu         sync.Mutex
	mu               sync.Mutex
	strongMu         sync.Mutex
	reserveMu        sync.Mutex
	joinMu           sync.Mutex
	nameReady        atomic.Bool
	started          bool
	ready            bool
	degraded         bool
}

// LocalPresence reads non-presence of a name in the LOCAL and EVENTUAL
// registries on the local node, bypassing the composed Lookup so it never
// re-enters globalreg (which would self-reference a held reservation). Wired at
// boot from topology.GetRegistry / topology.GetEventualRegistry; nil-safe (an
// unwired presence reports nothing bound, so the conditional ack degrades to an
// unconditional ack on a node with no local registries).
type LocalPresence interface {
	// LookupLocal reports a LOCAL-scope binding for name, if any.
	LookupLocal(name string) (pid.PID, bool)
	// LookupEventual reports an EVENTUAL-scope binding for name, if any.
	LookupEventual(name string) (pid.PID, bool)
}

// LocalNameRevoker revokes a conflicting LOCAL or EVENTUAL binding the join-epoch
// barrier discovered for a name a Strong reservation owns cluster-wide. The
// barrier calls it for each snapshot name this node holds bound to a different
// pid, before flipping name_ready. Wired at boot from the topology PIDRegistry
// (LOCAL) and the eventual registry (EVENTUAL); nil-safe (an unwired revoker is
// a no-op, so a node with no local registries still completes the barrier).
type LocalNameRevoker interface {
	// RevokeLocal removes a LOCAL-scope binding of name to a pid different from
	// keep, signaling the losing process. Returns true if a binding was revoked.
	RevokeLocal(name string, keep pid.PID) bool
	// RevokeEventual removes an EVENTUAL-scope binding of name to a pid different
	// from keep, signaling the losing process. Returns true if a binding was
	// revoked.
	RevokeEventual(name string, keep pid.PID) bool
}

// NewService creates a new global registry service.
//
// The trailing coll/mp/tp parameters wire OTel-style metrics for the
// pg_fence_*/pg_globalreg_* series consumed by the runtime dashboards. Pass
// nil for any of them to disable telemetry; the recorders are nil-safe.
// membership may be nil for unit tests that don't exercise Strong scope —
// any Strong-scope register will then fail loudly.
func NewService(
	raftSvc raftapi.Service,
	fsm *FSM,
	bus event.Bus,
	topo topology.Topology,
	router relay.Receiver,
	membership cluster.Membership,
	localNode pid.NodeID,
	logger *zap.Logger,
	coll metrics.Collector,
	mp otelmetric.MeterProvider,
	tp trace.TracerProvider,
) *Service {
	tel := newTelemetry(coll, mp, tp, localNode)
	if fsm != nil {
		fsm.SetTelemetry(tel)
	}

	s := &Service{
		raftSvc:          raftSvc,
		fsm:              fsm,
		tel:              tel,
		bus:              bus,
		topo:             topo,
		router:           router,
		membership:       membership,
		localNode:        localNode,
		logger:           logger,
		stopCh:           make(chan struct{}),
		pending:          make(map[uint64]chan *forwardResponse),
		forwardProxies:   make(map[uint64]pid.NodeID),
		strongWatchers:   make(map[string]map[uint64]chan strongOutcome),
		strongTimers:     make(map[string]*strongTimer),
		strongExclusions: make(map[string]strongExclusion),
		joinPending:      make(map[uint64]chan *joinResponseEnvelope),
		pingPending:      make(map[uint64]chan struct{}),
		ackerEpochs:      make(map[pid.NodeID]uint64),
	}
	if fsm != nil {
		fsm.SetOnRestore(s.resetMonitorWatermark)
		fsm.SetOnPending(s.handlePendingEvent)
		fsm.SetOnActive(s.handleActiveEvent)
		fsm.SetOnExpired(s.handleExpiredEvent)
		fsm.SetOnBinding(s.handleBindingEvent)
	}
	return s
}

// SetMembership wires the cluster membership service after construction.
// Boot wiring may construct the Service before the membership component
// finishes loading; this setter lets Start() install it just before the
// service is exposed.
func (s *Service) SetMembership(m cluster.Membership) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.membership = m
	s.mu.Unlock()
}

// SetLocalPresence wires the LOCAL/EVENTUAL presence reader used by the
// conditional ack. Boot installs it after the topology registries land in
// context. Safe for concurrent use.
func (s *Service) SetLocalPresence(lp LocalPresence) {
	if s == nil {
		return
	}
	s.localPresence.Store(lp)
}

func (s *Service) loadLocalPresence() LocalPresence {
	v := s.localPresence.Load()
	if v == nil {
		return nil
	}
	lp, ok := v.(LocalPresence)
	if !ok {
		return nil
	}
	return lp
}

// SetLeaderProbeConfig tunes the leader-reachability monitor. Zero values keep
// the defaults. Must be called before Start (the monitor reads these once at
// launch).
func (s *Service) SetLeaderProbeConfig(interval time.Duration, grace int) {
	if s == nil {
		return
	}
	if interval > 0 {
		s.probeInterval = interval
	}
	if grace > 0 {
		s.probeGrace = grace
	}
}

// SetLocalNameRevoker wires the LOCAL/EVENTUAL revoker the join-epoch barrier
// uses to drop conflicting names before flipping ready. Safe for concurrent use.
func (s *Service) SetLocalNameRevoker(r LocalNameRevoker) {
	if s == nil {
		return
	}
	s.localRevoker.Store(r)
}

func (s *Service) loadLocalRevoker() LocalNameRevoker {
	v := s.localRevoker.Load()
	if v == nil {
		return nil
	}
	r, ok := v.(LocalNameRevoker)
	if !ok {
		return nil
	}
	return r
}

// Start begins the service: subscribes to cluster events for auto-cleanup.
func (s *Service) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil, fmt.Errorf("global registry service already started")
	}
	s.started = true
	s.mu.Unlock()

	// Open the join-epoch barrier: fresh node epoch, name service not ready
	// until the barrier installs the leader's Strong snapshot and revokes
	// conflicting local names.
	s.nodeEpoch.Add(1)
	s.nameReady.Store(false)

	ch := make(chan event.Event, 32)
	subID, err := s.bus.SubscribeP(ctx, cluster.System, cluster.NodeLeft, ch)
	if err != nil {
		return nil, fmt.Errorf("subscribe to cluster events: %w", err)
	}
	go s.handleClusterEvents(ctx, ch, subID)

	// Rejoin trigger (#31): tie name-readiness to LEADER reachability, not peer
	// membership churn. A debounced probe closes the gate on a sustained leader
	// loss and re-runs the join barrier when reachability recovers, so a node
	// that stayed up through a partition re-learns strong names committed while
	// it was cut off instead of serving a stale conflict.
	go s.monitorLeaderReachability()

	go s.monitorLeadership()

	go s.waitForReady(ctx)

	go s.rebroadcastLoop()

	// Run the first-join barrier after Raft readiness so the leader snapshot is
	// authoritative. waitForReady runs the Raft barrier; the join barrier rides
	// behind it on its own goroutine.
	go s.joinBarrierOnStart()

	// Anti-entropy: periodic digest exchange with a derived raft member, so a
	// non-leader (especially a non-member) reconciles missed broadcasts. The
	// loop bails on stopCh; bounded one-peer-per-tick.
	go s.runAntiEntropy()

	// Tombstone GC: drop expired tombstones from the dissem cache. Hot only
	// once every tombstoneGCInterval, so cost is negligible.
	if d := s.loadDissem(); d != nil {
		go d.RunGC()
	}

	statusCh := make(chan any, 1)
	go func() {
		defer close(statusCh)
		<-s.stopCh
	}()

	s.logger.Info("global registry service started", zap.String("node", s.localNode))
	return statusCh, nil
}

// rebroadcastLoop periodically nudges pending entries on the leader. The
// cadence is keyed to StrongDeadline / strongRebroadcastDivisor so a 10s
// deadline produces ~2 nudges before expiry.
func (s *Service) rebroadcastLoop() {
	interval := global.StrongDeadline / strongRebroadcastDivisor
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			s.rebroadcastPending()
		}
	}
}

// Stop terminates the service.
func (s *Service) Stop(_ context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	s.mu.Unlock()

	close(s.stopCh)

	// Stop the dissem plane's GC goroutine, which runs off its own stopCh
	// (s.stopCh does not reach it).
	if d := s.loadDissem(); d != nil {
		d.Stop()
	}

	s.logger.Info("global registry service stopped", zap.String("node", s.localNode))
	return nil
}

// handleClusterEvents processes node-left events for auto-cleanup.
func (s *Service) handleClusterEvents(ctx context.Context, ch <-chan event.Event, subID event.SubscriberID) {
	defer s.bus.Unsubscribe(ctx, subID)

	for {
		select {
		case e, ok := <-ch:
			if !ok {
				return
			}
			if e.Kind == cluster.NodeLeft {
				s.handleNodeLeft(ctx, e)
			}
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) handleNodeLeft(ctx context.Context, e event.Event) {
	nodeEvt, ok := e.Data.(cluster.NodeEvent)
	if !ok {
		return
	}

	// Only the leader performs cleanup.
	if !s.raftSvc.IsLeader() {
		return
	}

	nodeID := nodeEvt.Node.ID
	s.logger.Info("removing global names for departed node", zap.String("node", nodeID))

	// Prune the departed node from every in-flight pending's RequiredNodes so
	// reservations that only awaited this node can still promote (or already
	// satisfied ones complete immediately in the drop Apply).
	s.dropDepartedFromPending(nodeID)

	// Drop the departed node's last-observed epoch so the map stays bounded
	// by the live cluster, not by every identity ever seen (matters under
	// ephemeral k8s pod hostnames + restart churn).
	s.strongMu.Lock()
	delete(s.ackerEpochs, nodeID)
	s.strongMu.Unlock()

	if err := s.RemoveNode(ctx, nodeID); err != nil {
		s.logger.Error("failed to remove node names", zap.String("node", nodeID), zap.Error(err))
	}
}

// dropDepartedFromPending issues a CmdDropRequired for every in-flight pending
// that still requires the departed node. Leader-only; idempotent on the FSM
// side so a duplicate event or a since-promoted entry is harmless.
func (s *Service) dropDepartedFromPending(nodeID pid.NodeID) {
	if s.fsm == nil {
		return
	}
	for _, v := range s.fsm.State().listPending() {
		requires := false
		for _, n := range v.RequiredNodes {
			if n == nodeID {
				requires = true
				break
			}
		}
		if !requires {
			continue
		}
		cmd := &Command{Type: CmdDropRequired, Name: v.Name, Epoch: v.Epoch, NodeID: nodeID}
		if _, err := s.applyCommand(cmd); err != nil {
			s.logger.Debug("globalreg: drop required failed",
				zap.String("name", v.Name), zap.Uint64("epoch", v.Epoch),
				zap.String("node", nodeID), zap.Error(err))
		}
	}
}

// --- global.Registry implementation ---

// Register registers a name at Consistent scope: the no-mode convenience over
// RegisterScope.
func (s *Service) Register(ctx context.Context, name string, p pid.PID) (pid.PID, error) {
	out, err := s.RegisterScope(ctx, name, p, global.Consistent)
	if err != nil {
		return out.ExistingPID, err
	}
	return out.PID, nil
}

// RegisterScope is the scope-aware register entrypoint.
func (s *Service) RegisterScope(ctx context.Context, name string, p pid.PID, mode global.RegistrationMode) (global.RegisterOutcome, error) {
	switch mode {
	case global.Consistent:
		return s.registerConsistent(name, p)
	case global.Strong:
		return s.registerStrong(ctx, name, p)
	case global.Local:
		return global.RegisterOutcome{}, fmt.Errorf("globalreg: Local scope not handled by global.Service")
	case global.Eventual:
		return global.RegisterOutcome{}, fmt.Errorf("globalreg: Eventual scope not handled by global.Service")
	default:
		return global.RegisterOutcome{}, fmt.Errorf("globalreg: unknown scope %d", mode)
	}
}

func (s *Service) registerConsistent(name string, p pid.PID) (global.RegisterOutcome, error) {
	nodeID := p.Node
	if nodeID == "" {
		nodeID = s.localNode
	}

	cmd := &Command{
		Type:   CmdRegister,
		Name:   name,
		PID:    p,
		NodeID: nodeID,
	}

	resp, err := s.applyCommand(cmd)
	if err != nil {
		return global.RegisterOutcome{}, err
	}

	result, ok := resp.(*RegisterResult)
	if !ok {
		return global.RegisterOutcome{}, fmt.Errorf("unexpected register response type: %T", resp)
	}

	if result.Err != nil {
		return global.RegisterOutcome{
			ExistingPID: result.ExistingPID,
		}, global.ErrNameAlreadyRegistered
	}

	if result.ResolvedPID != (pid.PID{}) {
		s.tel.recordReregistration(s.localNode, "global")
	}

	s.monitorPID(p)

	return global.RegisterOutcome{
		PID:   result.PID,
		Epoch: result.FenceToken,
		State: global.RegisterStateActive,
	}, nil
}

// Unregister removes a Consistent-scope registration: the no-mode convenience
// over UnregisterScope.
func (s *Service) Unregister(ctx context.Context, name string) (bool, error) {
	return s.UnregisterScope(ctx, name, global.Consistent)
}

// UnregisterScope removes a name from the given scope.
func (s *Service) UnregisterScope(_ context.Context, name string, mode global.RegistrationMode) (bool, error) {
	switch mode {
	case global.Consistent:
		cmd := &Command{Type: CmdUnregister, Name: name}
		resp, err := s.applyCommand(cmd)
		if err != nil {
			return false, err
		}
		r, ok := resp.(*UnregisterResult)
		if !ok {
			return false, fmt.Errorf("unexpected unregister response type: %T", resp)
		}
		return r.Removed, nil
	case global.Strong:
		cmd := &Command{Type: CmdRegisterUnreserve, Name: name}
		resp, err := s.applyCommand(cmd)
		if err != nil {
			return false, err
		}
		r, ok := resp.(*UnregisterResult)
		if !ok {
			return false, fmt.Errorf("unexpected unreserve response type: %T", resp)
		}
		return r.Removed, nil
	default:
		return false, fmt.Errorf("globalreg: UnregisterScope unsupported scope %d", mode)
	}
}

// NameReady reports whether the join-epoch barrier has completed for the current
// node epoch. Participating LOCAL/EVENTUAL register seams consult it; until it is
// true a register is refused with ErrNameServiceNotReady. A nil service reports
// ready so an unwired test path does not wedge.
func (s *Service) NameReady() bool {
	if s == nil {
		return true
	}
	return s.nameReady.Load()
}

// Lookup reads the registry from the local Raft FSM replica with optional
// behavior controlled by LookupOptions. See api/global.Registry.Lookup
// for the option semantics. Lookup is lock-free and O(1) for the no-options
// path; ByPID is O(shards) because it scans the reverse index.
//
// On FSM-miss for a name lookup the dissem cache is consulted: it carries
// gossip-replicated CONSISTENT/STRONG bindings the local node has not yet
// committed to its Raft replica (non-members never get the FSM at all).
// A tombstone in the cache surfaces as not-found. ByPID is unchanged — it
// always reads only the local FSM reverse index.
func (s *Service) Lookup(ctx context.Context, name string, opts ...global.LookupOption) (global.LookupResult, error) {
	var o global.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}

	state := s.fsm.State()

	if o.ByPID != nil {
		names := state.LookupByPID(*o.ByPID)
		return global.LookupResult{
			PID:         *o.ByPID,
			NamesForPID: names,
			Found:       len(names) > 0,
		}, nil
	}

	if p, found := state.Lookup(name); found {
		return global.LookupResult{PID: p, Found: true}, nil
	}
	if d := s.loadDissem(); d != nil {
		if p, found := d.Lookup(name); found {
			return global.LookupResult{PID: p, Found: true}, nil
		}
		// Cold-miss forward-resolve: a non-member that hasn't received the
		// broadcast yet asks a derived member for an authoritative answer.
		// Members keep the local FSM authoritative, so this branch fires only
		// on non-members (s.raftSvc.IsLeader() is false AND the FSM is empty;
		// resolveForwardTarget returns non-self candidates).
		if s.isNonMember() {
			if res, ok := s.forwardLookup(ctx, name); ok {
				return res, nil
			}
		}
	}
	return global.LookupResult{Found: false}, nil
}

// Remove removes all global names for a PID via Raft.
func (s *Service) Remove(_ context.Context, p pid.PID) error {
	cmd := &Command{
		Type: CmdRemovePID,
		PID:  p,
	}
	_, err := s.applyCommand(cmd)
	return err
}

// removeNodeChunkSize bounds the work per Raft Apply when bulk-removing
// a departed node's names. 256 names ≈ <5 ms per Apply; chunking lets other
// writes interleave during a large cleanup so the Raft pipeline doesn't
// stall under chaos-driven node churn.
const removeNodeChunkSize = 256

// RemoveNode removes all global names for a node via Raft. The work is
// chunked into bounded Applies so the FSM apply lock is released between
// batches, keeping foreground writes responsive while a node's state
// drains.
func (s *Service) RemoveNode(_ context.Context, nodeID pid.NodeID) error {
	for {
		cmd := &Command{
			Type:   CmdRemoveNode,
			NodeID: nodeID,
			Limit:  removeNodeChunkSize,
		}
		resp, err := s.applyCommand(cmd)
		if err != nil {
			return err
		}
		result, ok := resp.(*RemoveResult)
		if !ok {
			// Unexpected response shape — fall back to no further chunks.
			return nil
		}
		if s.tel != nil {
			s.tel.recordRemoveNodeChunk(nodeID, result.Count)
		}
		if !result.HasMore {
			return nil
		}
	}
}

// --- Internal ---

// Send implements relay.Receiver. It handles forwarded commands from other
// nodes and topology exit events for monitored PIDs.
func (s *Service) Send(pkg *relay.Package) error {
	defer relay.ReleasePackage(pkg)

	for _, msg := range pkg.Messages {
		switch msg.Topic {
		case topicForwardRequest:
			s.handleForwardRequest(pkg.Source, msg)
		case topicForwardResponse:
			s.handleForwardResponse(msg)
		case topicRegisterAck:
			s.handleRegisterAck(msg)
		case topicCheckPending:
			s.handleCheckPending(msg)
		case topicReleaseExclusion:
			s.handleReleaseExclusion(msg)
		case topicJoinRequest:
			s.handleJoinRequest(msg)
		case topicJoinResponse:
			s.handleJoinResponse(msg)
		case topicLeaderPing:
			s.handleLeaderPing(msg)
		case topicLeaderPong:
			s.handleLeaderPong(msg)
		case topicLookupRequest:
			s.handleLookupRequest(msg)
		case topicLookupResponse:
			s.handleLookupResponse(msg)
		case topicDigestExchange:
			s.handleDigestExchange(msg)
		case topicDigestDelta:
			s.handleDigestDelta(msg)
		case topology.TopicEvents:
			s.handleExitEvent(msg)
		}
	}

	return nil
}

// handleExitEvent processes topology exit events for monitored PIDs.
// When a globally registered process exits, its names are auto-removed.
func (s *Service) handleExitEvent(msg *relay.Message) {
	for _, p := range msg.Payloads {
		exitEvent, ok := p.Data().(*topology.ExitEvent)
		if !ok {
			continue
		}
		s.HandleProcessExit(exitEvent.From)
	}
}

// monitorPID starts topology monitoring for a globally registered PID.
// When the process exits, its names are auto-removed.
func (s *Service) monitorPID(p pid.PID) {
	if s.topo == nil {
		return
	}
	pidKey := p.String()
	if _, loaded := s.monitoredPIDs.LoadOrStore(pidKey, struct{}{}); loaded {
		return
	}

	self := pid.PID{Node: s.localNode, Host: HostID}
	if err := s.topo.Monitor(self, p); err != nil {
		s.logger.Debug("failed to monitor globally registered PID",
			zap.String("pid", pidKey), zap.Error(err))
		s.monitoredPIDs.Delete(pidKey)
	}
}

// HandleProcessExit is called when a monitored process exits.
// It removes all global names for that PID.
func (s *Service) HandleProcessExit(p pid.PID) {
	pidKey := p.String()
	if _, ok := s.monitoredPIDs.LoadAndDelete(pidKey); !ok {
		return
	}

	s.logger.Info("auto-removing global names for exited process", zap.String("pid", pidKey))
	if err := s.Remove(context.Background(), p); err != nil {
		s.logger.Error("failed to auto-remove process names", zap.String("pid", pidKey), zap.Error(err))
	}
}

// monitorLeadership watches for leadership changes and re-establishes
// PID monitors when this node becomes the leader. This ensures that after
// a leader failover, the new leader monitors all registered local PIDs
// for auto-cleanup on process exit.
func (s *Service) monitorLeadership() {
	leaderCh := s.raftSvc.LeaderCh()
	for {
		select {
		case isLeader, ok := <-leaderCh:
			if !ok {
				return
			}
			if isLeader {
				s.reestablishMonitors()
			}
		case <-s.stopCh:
			return
		}
	}
}

// reestablishMonitors scans entries newer than the last reestablish watermark
// and sets up topology monitors for PIDs local to this node. Bounding the
// scan by AppliedAt avoids a full FSM walk on every leader transition once
// a node has been around long enough to have monitored everything below the
// watermark. monitorPID() is itself idempotent via monitoredPIDs, so the
// watermark is purely a cost-cap: a leader failover with no new entries
// allocates nothing.
func (s *Service) reestablishMonitors() {
	threshold := s.monitorWatermark.Load()
	entries, highest := s.fsm.State().snapshotAbove(threshold)
	for _, e := range entries {
		if e.NodeID == s.localNode {
			s.monitorPID(e.PID)
		}
	}
	if highest > threshold {
		for {
			cur := s.monitorWatermark.Load()
			if highest <= cur {
				break
			}
			if s.monitorWatermark.CompareAndSwap(cur, highest) {
				break
			}
		}
	}

	s.resumeStrongTimers()
}

// resumeStrongTimers walks pending Strong entries and arms a deadline timer
// for each one this node now owns as leader. Called on leader transition.
func (s *Service) resumeStrongTimers() {
	if s.fsm == nil {
		return
	}
	for _, v := range s.fsm.State().listPending() {
		s.startStrongTimer(v.Name, v.Epoch, time.Unix(0, v.DeadlineUnixNano))
	}
}

// resetMonitorWatermark is invoked when the FSM is wholesale-replaced (e.g.
// snapshot install). Future reestablishMonitors calls must rescan from zero.
func (s *Service) resetMonitorWatermark() {
	s.monitorWatermark.Store(0)
}

// waitForReady waits for the Raft log to catch up using a barrier,
// then marks the service as ready to serve lookups.
// If the barrier times out, the service is marked as degraded (serving
// potentially stale data) but still ready, preferring availability over
// blocking indefinitely.
func (s *Service) waitForReady(_ context.Context) {
	if err := s.raftSvc.Barrier(30 * time.Second); err != nil {
		s.logger.Warn("raft barrier timed out during startup, serving lookups in degraded mode",
			zap.Error(err))
		s.mu.Lock()
		s.degraded = true
		s.ready = true
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	s.ready = true
	s.mu.Unlock()

	s.logger.Info("global registry ready (raft log caught up)")
}

// IsReady returns whether the service has caught up with the Raft log
// and can serve lookups.
func (s *Service) IsReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// IsDegraded returns whether the service started without fully catching up
// with the Raft log. Lookups may return stale data.
func (s *Service) IsDegraded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.degraded
}

// PendingSnapshot returns the current Strong-scope pending reservations.
// Read-only; intended for the admin endpoint.
func (s *Service) PendingSnapshot() []PendingView {
	if s == nil || s.fsm == nil {
		return nil
	}
	return s.fsm.State().listPending()
}

// ExpiredHistory returns the most recent expired reservations (capped).
func (s *Service) ExpiredHistory() []ExpiredRecord {
	if s == nil || s.fsm == nil {
		return nil
	}
	return s.fsm.State().expiredSnapshot()
}

// Ensure Service implements the interfaces.
var (
	_ global.Registry = (*Service)(nil)
	_ relay.Receiver  = (*Service)(nil)
)
