// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"

	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const (
	// HostID is the relay host ID for the global registry service.
	HostID pid.HostID = "globalreg"

	// Topics for inter-node leader forwarding.
	topicForwardRequest  relay.Topic = "globalreg.forward.request"
	topicForwardResponse relay.Topic = "globalreg.forward.response"
	// topicRegisterAck delivers a Strong-scope ack from a follower to the
	// current leader. Body is a msgpack-encoded ackEnvelope. The wire string
	// retains "root" as the on-the-wire identifier for the Strong scope.
	topicRegisterAck relay.Topic = "globalreg.root.ack"
	// topicCheckPending is an out-of-band relay nudge the leader sends to a
	// missing required node, asking it to re-evaluate the pending reservation
	// and re-emit its ack. Body is a msgpack-encoded checkPendingEnvelope.
	// The nudge never mutates Raft state — only ACK/REJECT/DROP/EXPIRED do.
	topicCheckPending relay.Topic = "globalreg.root.check"
	// topicReleaseExclusion is a targeted relay message the leader sends to a
	// node holding a Strong exclusion when the name reaches a terminal state
	// (expire/reject/unregister, including an active name being unregistered).
	// Body is a msgpack-encoded releaseEnvelope; the recipient releases its
	// exclusion for the carried (name, epoch) idempotently. Never mutates Raft.
	topicReleaseExclusion relay.Topic = "globalreg.root.release"
	// topicJoinRequest carries a node's JoinNameEpoch request to the leader. Body
	// is a msgpack-encoded joinRequestEnvelope. The leader replies on
	// topicJoinResponse with a snapshot of PENDING∪ACTIVE Strong names as of the
	// current commit index. Never mutates Raft; the wire string retains "root".
	topicJoinRequest relay.Topic = "globalreg.root.join"
	// topicJoinResponse delivers the leader's join snapshot back to the
	// requesting node. Body is a msgpack-encoded joinResponseEnvelope.
	topicJoinResponse relay.Topic = "globalreg.root.join.resp"
	// topicLeaderPing carries a minimal leader-reachability probe to the leader.
	// Body is a msgpack-encoded leaderPingEnvelope. The leader replies on
	// topicLeaderPong. A successful round-trip attests the leader is reachable
	// without building a snapshot — the cheap counterpart to JoinNameEpoch the
	// reachability monitor polls. Never mutates Raft; the wire string retains
	// "root".
	topicLeaderPing relay.Topic = "globalreg.root.ping"
	// topicLeaderPong delivers the leader's ping reply back to the prober.
	topicLeaderPong relay.Topic = "globalreg.root.pong"

	defaultApplyTimeout = 10 * time.Second

	// strongRebroadcastDivisor controls how often the leader re-broadcasts
	// pending entries. Set to 4 so a 10s deadline triggers ~2 rebroadcasts.
	strongRebroadcastDivisor = 4
)

// Service implements the globalreg.Registry interface.
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
	rebroadcastStop  chan struct{}
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

// strongOutcome is the value the Register caller blocks on while waiting for
// the FSM to commit Active or Expired. Reason/RejectedBy are set on a terminal
// reject so the caller surfaces a conflict distinct from a timeout.
type strongOutcome struct {
	Reason      string
	RejectedBy  pid.NodeID
	MissingAcks []pid.NodeID
	Epoch       uint64
	State       globalreg.RegisterState
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

// exclusionState tracks where a held Strong exclusion sits in its lifecycle.
type exclusionState uint8

const (
	// exclusionPending is latched when this node acks a Strong pending and holds
	// no conflicting binding. It blocks LOCAL/EVENTUAL registers of the name to a
	// different pid during the promotion window.
	exclusionPending exclusionState = iota
	// exclusionActive is the converted state after the name promotes to active.
	// The exclusion persists through promotion so a conflicting LOCAL/EVENTUAL
	// bind to a different pid is still refused while the name is authoritative.
	exclusionActive
)

// strongExclusion is a node-local attestation that this node acked a Strong
// reservation for a name at a given epoch and holds no conflicting binding. The
// epoch is the pending's Raft epoch — the instance id. The exclusion is installed
// Pending on ack, converted Pending->Active on promotion, and released only on a
// committed terminal FSM event matching the held epoch (indexed release).
type strongExclusion struct {
	pid   pid.PID
	epoch uint64
	state exclusionState
}

// strongTimer is the leader-side deadline goroutine for a pending Strong entry.
type strongTimer struct {
	deadline time.Time
	stop     chan struct{}
	name     string
	epoch    uint64
}

// ackEnvelope is the wire form of a Strong-scope ack delivered to the leader
// over the relay (topicRegisterAck). NodeEpoch is the acker's node epoch at the
// time it attested: the leader records it per acker so a later nudge can be
// addressed to the same incarnation and a stale (pre-rejoin) ack is detectable.
// Hop counts re-forward hops so a non-leader member that receives an ack while
// the leader-directed write plane is in election flux re-forwards once and
// stops. Zero on first send.
type ackEnvelope struct {
	Name      string     `codec:"n"`
	AckerNode pid.NodeID `codec:"a"`
	Epoch     uint64     `codec:"e"`
	NodeEpoch uint64     `codec:"ne"`
	Hop       uint8      `codec:"h,omitempty"`
}

// checkPendingEnvelope is the wire form of a leader → missing-node nudge
// (topicCheckPending). The recipient re-evaluates the pending (name, epoch) and
// re-emits its ack idempotently. NodeEpoch is the recipient's node epoch the
// leader last observed (0 when unknown); the recipient drops a nudge whose
// NodeEpoch is non-zero and does not match its current epoch — that nudge is
// addressed to a prior incarnation, and the join-epoch barrier re-learns the
// pending via the snapshot instead.
type checkPendingEnvelope struct {
	Name      string `codec:"n"`
	Epoch     uint64 `codec:"e"`
	NodeEpoch uint64 `codec:"ne"`
}

// releaseEnvelope is the wire form of a leader → exclusion-holder release
// (topicReleaseExclusion). The recipient releases its Strong exclusion for the
// carried (name, epoch) idempotently. The epoch makes the release indexed: a
// stale release for an older instance never clears a newer same-name exclusion.
type releaseEnvelope struct {
	Name  string `codec:"n"`
	Epoch uint64 `codec:"e"`
}

// forwardResponse wraps the result of a forwarded command. It carries the
// typed FSM response (Result) for v1 envelopes; older v0 envelopes set only
// ErrMsg and leave Result nil.
type forwardResponse struct {
	Result any
	ErrMsg string
	V1     bool
}

// correlationIDCounter generates unique correlation IDs for forwarded requests.
var correlationIDCounter atomic.Uint64

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
	s.rebroadcastStop = make(chan struct{})
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
	interval := globalreg.StrongDeadline / strongRebroadcastDivisor
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

// --- globalreg.Registry implementation ---

// Register registers a name at Consistent scope. Kept for back-compat
// callers that have not been migrated to RegisterScope.
func (s *Service) Register(ctx context.Context, name string, p pid.PID) (pid.PID, error) {
	out, err := s.RegisterScope(ctx, name, p, globalreg.Consistent)
	if err != nil {
		return out.ExistingPID, err
	}
	return out.PID, nil
}

// RegisterScope is the scope-aware register entrypoint.
func (s *Service) RegisterScope(ctx context.Context, name string, p pid.PID, mode globalreg.RegistrationMode) (globalreg.RegisterOutcome, error) {
	switch mode {
	case globalreg.Consistent:
		return s.registerConsistent(name, p)
	case globalreg.Strong:
		return s.registerStrong(ctx, name, p)
	case globalreg.Local:
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: Local scope not handled by globalreg.Service")
	case globalreg.Eventual:
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: Eventual scope not handled by globalreg.Service")
	default:
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: unknown scope %d", mode)
	}
}

func (s *Service) registerConsistent(name string, p pid.PID) (globalreg.RegisterOutcome, error) {
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
		return globalreg.RegisterOutcome{}, err
	}

	result, ok := resp.(*RegisterResult)
	if !ok {
		return globalreg.RegisterOutcome{}, fmt.Errorf("unexpected register response type: %T", resp)
	}

	if result.Err != nil {
		return globalreg.RegisterOutcome{
			ExistingPID: result.ExistingPID,
		}, globalreg.ErrNameAlreadyRegistered
	}

	if result.ResolvedPID != (pid.PID{}) {
		s.tel.recordReregistration(s.localNode, "global")
	}

	s.monitorPID(p)

	return globalreg.RegisterOutcome{
		PID:   result.PID,
		Epoch: result.FenceToken,
		State: globalreg.RegisterStateActive,
	}, nil
}

func (s *Service) registerStrong(ctx context.Context, name string, p pid.PID) (globalreg.RegisterOutcome, error) {
	if s.membership == nil {
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: Strong scope requires cluster membership")
	}

	nodeID := p.Node
	if nodeID == "" {
		nodeID = s.localNode
	}

	deadline := time.Now().Add(globalreg.StrongDeadline)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) && time.Until(ctxDeadline) >= 50*time.Millisecond {
		deadline = ctxDeadline
	}

	watchCh := make(chan strongOutcome, 1)
	s.installStrongWatcher(name, 0, watchCh)
	defer s.releaseStrongWatcher(name, 0, watchCh)

	// RequiredNodes is intentionally left empty: the leader stamps it from
	// its own membership at apply time (stampLeaderPending). This keeps the
	// quorum definition authoritative regardless of which node opened the
	// reservation.
	cmd := &Command{
		Type:             CmdRegisterPending,
		Name:             name,
		PID:              p,
		NodeID:           nodeID,
		DeadlineUnixNano: deadline.UnixNano(),
	}

	resp, err := s.applyCommand(cmd)
	if err != nil {
		return globalreg.RegisterOutcome{}, err
	}

	result, ok := resp.(*RegisterResult)
	if !ok {
		return globalreg.RegisterOutcome{}, fmt.Errorf("unexpected pending response type: %T", resp)
	}
	if result.Err != nil {
		return globalreg.RegisterOutcome{
			ExistingPID: result.ExistingPID,
		}, result.Err
	}

	epoch := result.FenceToken
	s.rebindStrongWatcher(name, 0, epoch)

	s.monitorPID(p)

	for {
		select {
		case <-ctx.Done():
			return globalreg.RegisterOutcome{Epoch: epoch}, ctx.Err()
		case <-s.stopCh:
			return globalreg.RegisterOutcome{Epoch: epoch}, globalreg.ErrNotAvailable
		case outcome := <-watchCh:
			switch outcome.State {
			case globalreg.RegisterStateActive:
				return globalreg.RegisterOutcome{
					PID:   p,
					Epoch: outcome.Epoch,
					State: globalreg.RegisterStateActive,
				}, nil
			case globalreg.RegisterStateExpired:
				if outcome.Reason == strongRejectConflict {
					return globalreg.RegisterOutcome{
							Epoch: outcome.Epoch,
							State: globalreg.RegisterStateExpired,
						}, &globalreg.StrongConflictError{
							Name:       name,
							Epoch:      outcome.Epoch,
							Reason:     outcome.Reason,
							RejectedBy: outcome.RejectedBy,
						}
				}
				missing := make([]string, len(outcome.MissingAcks))
				copy(missing, outcome.MissingAcks)
				return globalreg.RegisterOutcome{
						Epoch: outcome.Epoch,
						State: globalreg.RegisterStateExpired,
					}, &globalreg.StrongRegistrationTimeoutError{
						Name:        name,
						Epoch:       outcome.Epoch,
						MissingAcks: missing,
					}
			}
		}
	}
}

// Unregister removes a Consistent-scope registration. Back-compat shim.
func (s *Service) Unregister(ctx context.Context, name string) (bool, error) {
	return s.UnregisterScope(ctx, name, globalreg.Consistent)
}

// UnregisterScope removes a name from the given scope.
func (s *Service) UnregisterScope(_ context.Context, name string, mode globalreg.RegistrationMode) (bool, error) {
	switch mode {
	case globalreg.Consistent:
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
	case globalreg.Strong:
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

// snapshotRequiredNodes captures the live membership set used by the leader
// when opening a Strong reservation. Includes the local node. Sorted for
// deterministic encoding.
func (s *Service) snapshotRequiredNodes() []pid.NodeID {
	if s.membership == nil {
		return nil
	}
	nodes := s.membership.Nodes()
	out := make([]pid.NodeID, 0, len(nodes)+1)
	seen := make(map[pid.NodeID]struct{}, len(nodes)+1)
	for _, n := range nodes {
		if n.ID == "" {
			continue
		}
		nid := n.ID
		if _, dup := seen[nid]; dup {
			continue
		}
		seen[nid] = struct{}{}
		out = append(out, nid)
	}
	if _, dup := seen[s.localNode]; !dup && s.localNode != "" {
		out = append(out, s.localNode)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// installStrongWatcher registers a channel that will receive the strongOutcome
// for (name, epoch). epoch=0 is used briefly while the caller waits for the
// FSM to assign an epoch via the pending response.
func (s *Service) installStrongWatcher(name string, epoch uint64, ch chan strongOutcome) {
	s.strongMu.Lock()
	defer s.strongMu.Unlock()
	m, ok := s.strongWatchers[name]
	if !ok {
		m = make(map[uint64]chan strongOutcome, 1)
		s.strongWatchers[name] = m
	}
	m[epoch] = ch
}

// rebindStrongWatcher moves a watcher channel from one epoch key to another.
// Used after the caller learns the epoch assigned to its pending entry.
func (s *Service) rebindStrongWatcher(name string, fromEpoch, toEpoch uint64) {
	s.strongMu.Lock()
	defer s.strongMu.Unlock()
	m, ok := s.strongWatchers[name]
	if !ok {
		return
	}
	ch, ok := m[fromEpoch]
	if !ok {
		return
	}
	delete(m, fromEpoch)
	m[toEpoch] = ch
}

func (s *Service) releaseStrongWatcher(name string, epoch uint64, ch chan strongOutcome) {
	s.strongMu.Lock()
	defer s.strongMu.Unlock()
	m, ok := s.strongWatchers[name]
	if !ok {
		return
	}
	if cur, ok := m[epoch]; ok && cur == ch {
		delete(m, epoch)
	}
	for e, cur := range m {
		if cur == ch {
			delete(m, e)
		}
	}
	if len(m) == 0 {
		delete(s.strongWatchers, name)
	}
}

// deliverStrongOutcome wakes any watcher registered for (name, epoch).
func (s *Service) deliverStrongOutcome(name string, epoch uint64, outcome strongOutcome) {
	s.strongMu.Lock()
	m, ok := s.strongWatchers[name]
	if !ok {
		s.strongMu.Unlock()
		return
	}
	chans := make([]chan strongOutcome, 0, 2)
	if ch, ok := m[epoch]; ok {
		chans = append(chans, ch)
	}
	if ch, ok := m[0]; ok && epoch != 0 {
		chans = append(chans, ch)
	}
	s.strongMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- outcome:
		default:
		}
	}
}

// handlePendingEvent runs on every replica from FSM.Apply.
// A required node attests it holds no conflicting LOCAL or EVENTUAL binding
// before acking, latching a reservation so it cannot create one during the
// promotion window. A conflict sends a terminal NACK instead of an ack. The
// leader applies its ack/reject directly (asynchronously, since Apply is
// single-threaded and re-entering would deadlock); a follower forwards it over
// the relay.
func (s *Service) handlePendingEvent(ev PendingEvent) {
	inSet := false
	for _, n := range ev.RequiredNodes {
		if n == s.localNode {
			inSet = true
			break
		}
	}
	if !inSet {
		if s.raftSvc != nil && s.raftSvc.IsLeader() {
			s.startStrongTimer(ev.Name, ev.Epoch, time.Unix(0, ev.DeadlineUnixNano))
		}
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		s.startStrongTimer(ev.Name, ev.Epoch, time.Unix(0, ev.DeadlineUnixNano))
	}
	// Async so the ack/reject Raft write never re-enters FSM.Apply (the leader)
	// and the relay send never blocks Apply (a follower).
	go s.evaluatePending(ev.Name, ev.Epoch, ev.PID)
}

// evaluatePending performs the conditional ack: check local non-presence and
// latch a reservation, then ack; or, on conflict, send a terminal NACK and ack
// nothing. The check+latch run under reserveMu so a competing latch cannot slip
// between "checked absent" and "reserved". The latch and the cross-scope
// IsStrongReserved guard (LOCAL PIDRegistry, EVENTUAL register) form a two-sided
// check: each side reads the other before committing. They run under separate
// locks, so a register racing the latch is resolved by whichever observes the
// other first — a single residual window inherent to the AP/CP boundary, not a
// shared critical section.
func (s *Service) evaluatePending(name string, epoch uint64, pendingPID pid.PID) {
	reserved := s.reserveCheckAndLatch(name, pendingPID, epoch, func() (pid.PID, bool) {
		return s.localConflict(name, pendingPID)
	})
	if !reserved {
		s.sendReject(name, epoch, strongRejectConflict)
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		s.applySelfAck(name, epoch)
		return
	}
	if err := s.sendAck(name, epoch); err != nil {
		s.logger.Debug("globalreg: send strong ack failed",
			zap.String("name", name), zap.Uint64("epoch", epoch), zap.Error(err))
	}
}

// localConflict reports a conflicting LOCAL or EVENTUAL binding: a binding of
// name to a pid DIFFERENT from pendingPID. A binding to the same pid is not a
// conflict. Returns the conflicting pid and true when a conflict exists.
func (s *Service) localConflict(name string, pendingPID pid.PID) (pid.PID, bool) {
	lp := s.loadLocalPresence()
	if lp == nil {
		return pid.PID{}, false
	}
	if p, ok := lp.LookupLocal(name); ok && p != pendingPID {
		return p, true
	}
	if p, ok := lp.LookupEventual(name); ok && p != pendingPID {
		return p, true
	}
	return pid.PID{}, false
}

// reserveCheckAndLatch closes the window between checking local non-presence
// and latching an exclusion. conflict is invoked under reserveMu; if it
// reports a conflict no exclusion is latched and false is returned (the caller
// must NACK). Otherwise a Pending exclusion for (name, epoch) -> pendingPID is
// installed idempotently and true is returned. An existing exclusion for the
// same name+epoch is treated as already satisfied regardless of its state.
func (s *Service) reserveCheckAndLatch(name string, pendingPID pid.PID, epoch uint64, conflict func() (pid.PID, bool)) bool {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	if existing, ok := s.strongExclusions[name]; ok && existing.epoch == epoch {
		return true
	}
	if _, bad := conflict(); bad {
		return false
	}
	s.strongExclusions[name] = strongExclusion{pid: pendingPID, epoch: epoch, state: exclusionPending}
	return true
}

// promoteExclusion converts a held exclusion from Pending to Active for the
// matching (name, epoch) and keeps it. Promotion (PENDING->ACTIVE) must not drop
// the exclusion: a conflicting LOCAL/EVENTUAL bind to a different pid stays
// refused while the name is authoritative. A mismatched epoch is a no-op so a
// stale activation never disturbs a newer instance.
func (s *Service) promoteExclusion(name string, epoch uint64) {
	s.reserveMu.Lock()
	if e, ok := s.strongExclusions[name]; ok && e.epoch == epoch {
		e.state = exclusionActive
		s.strongExclusions[name] = e
	}
	s.reserveMu.Unlock()
}

// releaseExclusion drops a held exclusion for name only when its epoch matches
// the terminal event's. A mismatched epoch leaves a newer exclusion intact
// (indexed release). Released on a committed terminal FSM event or a leader
// release delivery — never on a local timeout.
func (s *Service) releaseExclusion(name string, epoch uint64) {
	s.reserveMu.Lock()
	if e, ok := s.strongExclusions[name]; ok && e.epoch == epoch {
		delete(s.strongExclusions, name)
	}
	s.reserveMu.Unlock()
}

// isStrongReserved reports a held exclusion for name in EITHER state
// (test/internal read).
func (s *Service) isStrongReserved(name string) (pid.PID, bool) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	e, ok := s.strongExclusions[name]
	return e.pid, ok
}

// IsStrongReserved reports whether this node holds a Strong exclusion for name
// in either the Pending (acked, awaiting promotion) or Active (promoted)
// state, surfacing the owning pid as taken. Cross-scope register guards (LOCAL
// PIDRegistry, EVENTUAL crossScopeChecker) consult it through the existing
// topology.GlobalRegistry handle so a name held by a Strong reservation cannot
// be granted to a different pid — through promotion and until a true terminal.
func (s *Service) IsStrongReserved(name string) (pid.PID, bool) {
	if s == nil {
		return pid.PID{}, false
	}
	return s.isStrongReserved(name)
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

// sendReject emits a terminal NACK for a pending reservation. The leader
// applies CmdRegisterReject directly; a follower forwards it via the relay,
// mirroring sendAck. Reject dominates acks in the FSM.
func (s *Service) sendReject(name string, epoch uint64, reason string) {
	cmd := &Command{
		Type:      CmdRegisterReject,
		Name:      name,
		Epoch:     epoch,
		AckerNode: s.localNode,
		Reason:    reason,
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		if _, err := s.applyCommand(cmd); err != nil {
			s.logger.Debug("globalreg: self-reject failed",
				zap.String("name", name), zap.Uint64("epoch", epoch), zap.Error(err))
		}
		return
	}
	if err := s.forwardReject(cmd); err != nil {
		s.logger.Debug("globalreg: forward strong reject failed",
			zap.String("name", name), zap.Uint64("epoch", epoch), zap.Error(err))
	}
}

// forwardReject sends a CmdRegisterReject to the leader-directed write plane.
// Rides the same forwardToLeader candidate-list path acks and registers use,
// so a non-member's NACK still reaches the FSM through a member relay.
func (s *Service) forwardReject(cmd *Command) error {
	if _, err := s.forwardToLeader(cmd); err != nil {
		return err
	}
	return nil
}

// applySelfAck records the leader's own ack via Raft. Callers invoke it off the
// FSM.Apply goroutine (evaluatePending runs async; handleCheckPending runs on
// the relay path) so the Raft write never re-enters Apply.
func (s *Service) applySelfAck(name string, epoch uint64) {
	s.recordAckerEpoch(s.localNode, s.nodeEpoch.Load())
	cmd := &Command{
		Type:      CmdRegisterAck,
		Name:      name,
		Epoch:     epoch,
		AckerNode: s.localNode,
	}
	if _, err := s.applyCommand(cmd); err != nil {
		s.logger.Debug("globalreg: self-ack failed",
			zap.String("name", name), zap.Uint64("epoch", epoch), zap.Error(err))
	}
}

// handleActiveEvent wakes any local Register caller waiting on this entry and
// converts the held exclusion Pending->Active so it persists through promotion.
// The outcome carries the activation Raft index (ev.ActivationIdx) as the
// registration epoch. Promotion is NOT a terminal — the exclusion is kept.
func (s *Service) handleActiveEvent(ev ActiveEvent) {
	s.stopStrongTimer(ev.Name, ev.Epoch)
	s.promoteExclusion(ev.Name, ev.Epoch)
	s.deliverStrongOutcome(ev.Name, ev.Epoch, strongOutcome{
		State: globalreg.RegisterStateActive,
		Epoch: ev.ActivationIdx,
	})
}

// handleExpiredEvent wakes any local Register caller waiting on this entry and
// releases the held exclusion (indexed to ev.Epoch). This is the single terminal
// path for expire, reject, unreserve, pid-exit, and an active name being
// unregistered. A reject (RejectedBy set, reason "conflict") and a timeout both
// arrive as RegisterStateExpired; the caller distinguishes them via
// outcome.Reason. The leader also delivers a release to the exclusion holders
// (RequiredNodes) so a non-member that latched via the nudge clears its
// exclusion instead of blocking the name forever.
func (s *Service) handleExpiredEvent(ev ExpiredEvent) {
	s.stopStrongTimer(ev.Name, ev.Epoch)
	s.releaseExclusion(ev.Name, ev.Epoch)
	s.deliverReleaseToHolders(ev.Name, ev.Epoch, ev.RequiredNodes)
	s.deliverStrongOutcome(ev.Name, ev.Epoch, strongOutcome{
		State:       globalreg.RegisterStateExpired,
		Epoch:       ev.Epoch,
		MissingAcks: ev.MissingAcks,
		Reason:      ev.Reason,
		RejectedBy:  ev.RejectedBy,
	})
}

// deliverReleaseToHolders sends a targeted exclusion release to every holder of
// the named Strong reservation. The leader knows the holders (RequiredNodes);
// the local node releases via handleExpiredEvent's own releaseExclusion, so a
// send to self is skipped. Idempotent and non-leader-safe (only the leader has
// authoritative RequiredNodes and emits the terminal event).
func (s *Service) deliverReleaseToHolders(name string, epoch uint64, holders []pid.NodeID) {
	if s.raftSvc == nil || !s.raftSvc.IsLeader() {
		return
	}
	for _, h := range holders {
		if h == s.localNode || h == "" {
			continue
		}
		if err := s.sendReleaseExclusion(h, name, epoch); err != nil {
			s.logger.Debug("globalreg: release exclusion send failed",
				zap.String("name", name), zap.Uint64("epoch", epoch),
				zap.String("target", h), zap.Error(err))
		}
	}
}

// sendAck delivers a Strong-scope ack to the leader-directed write plane. The
// recipient is either the authoritative leader (1-hop fast path) or, on a
// non-member or election window, a deterministic raft member that re-forwards
// once to its authoritative Leader(). Tries candidates in order; succeeds when
// the first send completes (acks are fire-and-forget — the leader records the
// ack from FSM.Apply, and a duplicate ack is idempotent on the FSM side).
func (s *Service) sendAck(name string, epoch uint64) error {
	targets, err := s.resolveForwardTarget()
	if err != nil {
		return err
	}
	body, err := marshalMsgpack(ackEnvelope{
		Name:      name,
		Epoch:     epoch,
		AckerNode: s.localNode,
		NodeEpoch: s.nodeEpoch.Load(),
	})
	if err != nil {
		return err
	}
	var lastErr error
	for _, target := range targets {
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicRegisterAck,
			payload.New(body),
		)
		if sendErr := s.router.Send(pkg); sendErr != nil {
			relay.ReleasePackage(pkg)
			lastErr = sendErr
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no targets accepted strong ack")
	}
	return lastErr
}

// startStrongTimer is called by the leader to arm a deadline timer for a
// pending entry. Stopping the timer is safe to call from a different
// goroutine; the timer goroutine exits cleanly on stop.
func (s *Service) startStrongTimer(name string, epoch uint64, deadline time.Time) {
	key := strongTimerKey(name, epoch)
	stop := make(chan struct{})
	s.strongMu.Lock()
	if existing, ok := s.strongTimers[key]; ok {
		close(existing.stop)
	}
	t := &strongTimer{name: name, epoch: epoch, stop: stop, deadline: deadline}
	s.strongTimers[key] = t
	s.strongMu.Unlock()

	go func() {
		wait := time.Until(deadline)
		if wait < 0 {
			wait = 0
		}
		select {
		case <-time.After(wait):
			if !s.raftSvc.IsLeader() {
				return
			}
			s.fireStrongExpire(name, epoch, "missing_ack")
		case <-stop:
			return
		case <-s.stopCh:
			return
		}
	}()
}

func (s *Service) stopStrongTimer(name string, epoch uint64) {
	key := strongTimerKey(name, epoch)
	s.strongMu.Lock()
	if t, ok := s.strongTimers[key]; ok {
		close(t.stop)
		delete(s.strongTimers, key)
	}
	s.strongMu.Unlock()
}

func (s *Service) fireStrongExpire(name string, epoch uint64, reason string) {
	cmd := &Command{Type: CmdRegisterExpired, Name: name, Epoch: epoch, Reason: reason}
	if _, err := s.applyCommand(cmd); err != nil {
		s.logger.Warn("globalreg: fire strong expire failed",
			zap.String("name", name), zap.Uint64("epoch", epoch),
			zap.String("reason", reason), zap.Error(err))
	}
}

func strongTimerKey(name string, epoch uint64) string {
	return fmt.Sprintf("%s|%d", name, epoch)
}

// rebroadcastPending nudges every missing required node of each in-flight
// pending entry to re-emit its ack. The nudge is an out-of-band relay message
// (topicCheckPending) — it never re-applies CmdRegisterPending, so it adds no
// Raft writes. Only ACK/REJECT/DROP/EXPIRED mutate the FSM. Leader-only.
func (s *Service) rebroadcastPending() {
	if !s.raftSvc.IsLeader() {
		return
	}
	views := s.fsm.State().listPending()
	now := time.Now()
	for _, v := range views {
		if len(v.MissingAcks) == 0 {
			continue
		}
		if now.UnixNano() >= v.DeadlineUnixNano {
			continue
		}
		for _, missing := range v.MissingAcks {
			if missing == s.localNode {
				// The leader nudges itself by re-running its conditional ack.
				go s.evaluatePending(v.Name, v.Epoch, v.PID)
				continue
			}
			if err := s.sendPendingNudge(missing, v); err != nil {
				s.logger.Debug("globalreg: pending nudge failed",
					zap.String("name", v.Name), zap.Uint64("epoch", v.Epoch),
					zap.String("target", missing), zap.Error(err))
			}
		}
	}
}

// recordAckerEpoch stores the node epoch an acker attested at so a later nudge
// can be addressed to the same incarnation (leader-side, idempotent). A lower
// epoch never overwrites a higher one — acks may arrive out of order.
func (s *Service) recordAckerEpoch(node pid.NodeID, epoch uint64) {
	if node == "" || epoch == 0 {
		return
	}
	s.strongMu.Lock()
	if cur, ok := s.ackerEpochs[node]; !ok || epoch > cur {
		s.ackerEpochs[node] = epoch
	}
	s.strongMu.Unlock()
}

func (s *Service) lookupAckerEpoch(node pid.NodeID) uint64 {
	s.strongMu.Lock()
	defer s.strongMu.Unlock()
	return s.ackerEpochs[node]
}

// sendPendingNudge sends a targeted CheckStrongPending relay message to a
// missing required node. The recipient re-evaluates and re-acks idempotently.
// The nudge echoes the recipient's last-observed node epoch (0 when unknown) so
// the recipient can drop a nudge addressed to a prior incarnation. Does not
// touch Raft.
func (s *Service) sendPendingNudge(targetNode pid.NodeID, v PendingView) error {
	body, err := marshalMsgpack(checkPendingEnvelope{
		Name:      v.Name,
		Epoch:     v.Epoch,
		NodeEpoch: s.lookupAckerEpoch(targetNode),
	})
	if err != nil {
		return err
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		targetNode, HostID,
		topicCheckPending,
		payload.New(body),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	return nil
}

// handleCheckPending processes a leader nudge for a missing required node. It
// re-runs the conditional ack for the named pending reservation: the node
// re-attests local non-presence and re-latches its reservation before re-acking,
// or sends a terminal NACK on a conflict. recordAck/rejectPending make the
// outcome idempotent: a duplicate ack is a no-op and a duplicate reject on an
// already-removed entry is a no-op. A node that no longer holds the pending
// entry (already promoted/expired) is a no-op via the epoch / unknown-name guard.
func (s *Service) handleCheckPending(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env checkPendingEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed pending nudge", zap.Error(err))
		return
	}
	// Drop a nudge addressed to a prior incarnation: it carries a node epoch the
	// leader observed before this node rejoined. The join-epoch barrier re-learns
	// the pending via the snapshot and installs the exclusion instead.
	if env.NodeEpoch != 0 && env.NodeEpoch != s.nodeEpoch.Load() {
		return
	}
	pv := s.fsm.State().pendingByName(env.Name)
	if pv == nil || pv.Epoch != env.Epoch {
		// Already promoted/expired or stale epoch — nothing to re-evaluate.
		return
	}
	s.evaluatePending(env.Name, env.Epoch, pv.PID)
}

// sendReleaseExclusion sends a targeted release to a node holding a Strong
// exclusion for (name, epoch). The recipient releases its exclusion idempotently.
// Does not touch Raft.
func (s *Service) sendReleaseExclusion(targetNode pid.NodeID, name string, epoch uint64) error {
	body, err := marshalMsgpack(releaseEnvelope{Name: name, Epoch: epoch})
	if err != nil {
		return err
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		targetNode, HostID,
		topicReleaseExclusion,
		payload.New(body),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	return nil
}

// handleReleaseExclusion processes a leader release delivery. The node releases
// its held Strong exclusion for the carried (name, epoch). The release is
// indexed: a stale release for an older instance leaves a newer same-name
// exclusion intact. Idempotent — releasing a name this node never held is a
// no-op.
func (s *Service) handleReleaseExclusion(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env releaseEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed exclusion release", zap.Error(err))
		return
	}
	s.releaseExclusion(env.Name, env.Epoch)
}

// Lookup reads the registry from the local Raft FSM replica with optional
// behavior controlled by LookupOptions. See api/globalreg.Registry.Lookup
// for the option semantics. Lookup is lock-free and O(1) for the no-options
// path; ByPID is O(shards) because it scans the reverse index.
//
// On FSM-miss for a name lookup the dissem cache is consulted: it carries
// gossip-replicated CONSISTENT/STRONG bindings the local node has not yet
// committed to its Raft replica (non-members never get the FSM at all).
// A tombstone in the cache surfaces as not-found. ByPID is unchanged — it
// always reads only the local FSM reverse index.
func (s *Service) Lookup(ctx context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error) {
	var o globalreg.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}

	state := s.fsm.State()

	if o.ByPID != nil {
		names := state.LookupByPID(*o.ByPID)
		return globalreg.LookupResult{
			PID:         *o.ByPID,
			NamesForPID: names,
			Found:       len(names) > 0,
		}, nil
	}

	if p, found := state.Lookup(name); found {
		return globalreg.LookupResult{PID: p, Found: true}, nil
	}
	if d := s.loadDissem(); d != nil {
		if p, found := d.Lookup(name); found {
			return globalreg.LookupResult{PID: p, Found: true}, nil
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
	return globalreg.LookupResult{Found: false}, nil
}

// Deprecated: use Lookup(ctx, "", globalreg.ByPID(p)).
func (s *Service) LookupByPID(p pid.PID) []string {
	r, _ := s.Lookup(context.Background(), "", globalreg.ByPID(p))
	return r.NamesForPID
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

// stampLeaderPending stamps RequiredNodes onto a fresh Strong open from the
// leader's membership. Only the leader stamps, and only a fresh open (Epoch
// unassigned, no RequiredNodes yet). An already-committed pending is never
// re-stamped: a new leader inherits the committed RequiredNodes because fresh
// opens are the only CmdRegisterPending the protocol applies. This keeps
// FSM.Apply deterministic — the set is fixed once, then replicated verbatim.
func (s *Service) stampLeaderPending(cmd *Command) {
	if cmd == nil || cmd.Type != CmdRegisterPending {
		return
	}
	if cmd.Epoch != 0 || len(cmd.RequiredNodes) > 0 {
		return
	}
	if s.raftSvc == nil || !s.raftSvc.IsLeader() {
		return
	}
	cmd.RequiredNodes = s.snapshotRequiredNodes()
}

// applyCommand encodes and proposes a command to Raft.
// If this node is not the leader, the command is forwarded via relay.
func (s *Service) applyCommand(cmd *Command) (any, error) {
	s.stampLeaderPending(cmd)

	data, err := EncodeCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("encode command: %w", err)
	}

	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	if err == nil {
		// FSM.Apply may return an error wrapped in a result struct.
		if resp.Response != nil {
			if fsmErr, ok := resp.Response.(error); ok {
				return nil, fsmErr
			}
		}
		return resp.Response, nil
	}

	// Not the leader — forward to leader via relay.
	if errors.Is(err, raftapi.ErrNotLeader) {
		return s.forwardToLeader(cmd)
	}

	return nil, err
}

// forwardToLeader sends a leader-directed command over the relay, trying each
// resolveForwardTarget candidate in order until one responds. The first
// candidate is the authoritative leader (raftSvc.Leader()) when known; the
// remainder are deterministic raft members derived from the gossip view, used
// as the shared write plane for non-members that never observe a leader
// directly. Uses a correlation ID to pair the response with this caller and
// retries the next candidate on send failure or per-attempt timeout.
//
// The forward envelope carries an 8B corrID, a 1B hop count, and the encoded
// command. A member receiving a hop=0 envelope while NOT the leader
// re-forwards it once to its authoritative Leader() (hop becomes 1) and relays
// the response on the same corrID back to the original requester. A hop>=1
// recipient that is not the leader replies an error so a stale-leader window
// cannot infinite-loop.
func (s *Service) forwardToLeader(cmd *Command) (any, error) {
	start := time.Now()

	// Discover candidates. Retry briefly because membership and leader state
	// may need a beat to settle after start/rejoin.
	var (
		targets []pid.NodeID
		lastErr error
	)
	backoff := 100 * time.Millisecond
	for i := 0; i < 30; i++ {
		t, err := s.resolveForwardTarget()
		if err == nil && len(t) > 0 {
			targets = t
			break
		}
		lastErr = err
		select {
		case <-s.stopCh:
			s.tel.recordForwardedApply(cmd.Type, forwardResultNoLeader, time.Since(start))
			return nil, globalreg.ErrNotAvailable
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > time.Second {
			backoff = time.Second
		}
	}
	if len(targets) == 0 {
		s.tel.recordForwardedApply(cmd.Type, forwardResultNoLeader, time.Since(start))
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, globalreg.ErrNotAvailable
	}

	data, err := EncodeCommand(cmd)
	if err != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
		return nil, fmt.Errorf("encode forward command: %w", err)
	}

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *forwardResponse, 1)

	s.mu.Lock()
	s.pending[corrID] = respCh
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, corrID)
		s.mu.Unlock()
	}()

	// Per-attempt timeout: defaultApplyTimeout split across at most a small
	// set of candidates so the caller's effective wall-clock stays bounded.
	// One re-forward hop adds at most a single internal round-trip per attempt.
	attempts := len(targets)
	if attempts > 3 {
		attempts = 3
	}
	perAttempt := defaultApplyTimeout / time.Duration(attempts)
	if perAttempt < time.Second {
		perAttempt = time.Second
	}

	envelope := encodeForwardRequest(corrID, 0, data)

	var sendErr error
	for i, target := range targets {
		if i >= attempts {
			break
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicForwardRequest,
			payload.New(envelope),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			sendErr = err
			continue
		}

		select {
		case resp := <-respCh:
			if resp.ErrMsg != "" {
				s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
				return nil, errors.New(resp.ErrMsg)
			}
			s.tel.recordForwardedApply(cmd.Type, forwardResultOK, time.Since(start))
			return resp.Result, nil
		case <-time.After(perAttempt):
			sendErr = globalreg.ErrNotAvailable
			continue
		case <-s.stopCh:
			s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
			return nil, globalreg.ErrNotAvailable
		}
	}

	if sendErr != nil {
		s.tel.recordForwardedApply(cmd.Type, forwardResultSendFailed, time.Since(start))
		return nil, fmt.Errorf("forward to leader: %w", sendErr)
	}
	s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
	return nil, globalreg.ErrNotAvailable
}

// encodeForwardRequest packs the leader-directed forward envelope:
//
//	[8B corrID][1B hop][command bytes]
//
// The hop byte gates the no-loop guarantee in handleForwardRequest: a member
// receiving the envelope while not the leader bumps hop and re-forwards once
// to its authoritative Leader(); a hop>=maxForwardHops recipient that is
// still not the leader returns an error instead of looping.
func encodeForwardRequest(corrID uint64, hop uint8, data []byte) []byte {
	out := make([]byte, 8+1+len(data))
	binary.BigEndian.PutUint64(out[:8], corrID)
	out[8] = hop
	copy(out[9:], data)
	return out
}

// decodeForwardRequest unpacks a forward envelope and returns the corrID,
// hop count, and command bytes. Returns false when the envelope is too
// short to be valid.
func decodeForwardRequest(envelope []byte) (corrID uint64, hop uint8, data []byte, ok bool) {
	if len(envelope) < 9 {
		return 0, 0, nil, false
	}
	return binary.BigEndian.Uint64(envelope[:8]), envelope[8], envelope[9:], true
}

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

// handleRegisterAck routes an ack arriving over the relay. The leader applies
// it through Raft (idempotent). A non-leader member receiving an ack acts as
// the shared write plane for non-members: it re-forwards the envelope once to
// its authoritative Leader(). A second non-leader recipient (hop>=cap) drops
// the ack — duplicate acks are idempotent on the FSM side so a single missed
// hop is harmless, and the leader's rebroadcast loop will nudge the missing
// node back into the protocol on the next tick.
func (s *Service) handleRegisterAck(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env ackEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed strong ack", zap.Error(err))
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		s.recordAckerEpoch(env.AckerNode, env.NodeEpoch)
		cmd := &Command{
			Type:      CmdRegisterAck,
			Name:      env.Name,
			Epoch:     env.Epoch,
			AckerNode: env.AckerNode,
		}
		if _, err := s.applyCommand(cmd); err != nil {
			s.logger.Debug("globalreg: apply strong ack failed",
				zap.String("name", env.Name), zap.Uint64("epoch", env.Epoch), zap.Error(err))
		}
		return
	}
	next, ok := s.reForwardTarget(env.Hop)
	if !ok {
		return
	}
	env.Hop++
	relayBody, err := marshalMsgpack(env)
	if err != nil {
		return
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		next, HostID,
		topicRegisterAck,
		payload.New(relayBody),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg: re-forward strong ack failed",
			zap.String("name", env.Name), zap.Uint64("epoch", env.Epoch),
			zap.String("to", next), zap.Error(err))
	}
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

// handleForwardRequest processes a forwarded command. The recipient is either
// the leader (apply directly and reply) or a non-leader raft member acting as
// the shared write plane for a non-member requester (re-forward once to the
// authoritative leader, relay the response back on the original corrID).
//
// The hop byte in the envelope caps the re-forward chain at maxForwardHops:
// a hop>=cap recipient that is still not the leader returns an error rather
// than re-forwarding, eliminating any stale-leader-induced loop.
func (s *Service) handleForwardRequest(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok {
		s.logger.Error("invalid forward request payload")
		return
	}

	corrID, hop, data, valid := decodeForwardRequest(envelope)
	if !valid {
		s.logger.Error("invalid forward request payload")
		return
	}

	// Re-forward path: a non-leader member receiving a hop<cap envelope
	// forwards once to its authoritative Leader() and proxies the response
	// back to the original requester on the same corrID. Members observe
	// leadership instantly via AppendEntries, so this is election-safe even
	// when the original requester's derived list pointed here from a stale
	// leader hint.
	if s.raftSvc == nil || !s.raftSvc.IsLeader() {
		next, ok := s.reForwardTarget(hop)
		if !ok {
			// Cap hit or no leader known on this member: surface a typed
			// error instead of dropping. The original requester sees the
			// failure on its corrID and falls back to the next candidate.
			s.replyForward(source.Node, corrID, 0, raftapi.ErrNotLeader.Error(), nil)
			return
		}
		s.proxyForwardRequest(source.Node, corrID, hop+1, data, next)
		return
	}

	// Decode the command so we can tag the typed response with its kind. The
	// leader-side apply path itself does not need the decoded command — Raft
	// decodes it again inside FSM.Apply — but the response envelope must
	// carry the command kind so the follower knows how to decode the typed
	// result blob.
	cmd, decodeErr := DecodeCommand(data)
	if decodeErr != nil {
		s.logger.Error("invalid forward request command", zap.Error(decodeErr))
		s.replyForward(source.Node, corrID, 0, decodeErr.Error(), nil)
		return
	}

	// Stamp RequiredNodes from the leader's membership for a forwarded fresh
	// Strong open. The follower deliberately omits the set so the leader's
	// view is authoritative; re-encode so the stamped set lands in the log.
	if cmd.Type == CmdRegisterPending && cmd.Epoch == 0 && len(cmd.RequiredNodes) == 0 {
		s.stampLeaderPending(cmd)
		if restamped, encErr := EncodeCommand(cmd); encErr == nil {
			data = restamped
		} else {
			s.logger.Error("re-encode stamped forward command", zap.Error(encErr))
		}
	}

	var (
		errMsg     string
		resultBlob []byte
	)
	resp, err := s.raftSvc.Apply(data, defaultApplyTimeout)
	switch {
	case err != nil:
		errMsg = err.Error()
	case resp == nil || resp.Response == nil:
		// No-op: nothing to encode.
	default:
		if fsmErr, ok := resp.Response.(error); ok {
			errMsg = fsmErr.Error()
			break
		}
		encoded, encErr := encodeFSMResult(cmd.Type, resp.Response)
		if encErr != nil {
			s.logger.Error("encode forward response result",
				zap.Error(encErr), zap.String("cmd", commandLabel(cmd.Type)))
			errMsg = encErr.Error()
			break
		}
		resultBlob = encoded
	}

	s.replyForward(source.Node, corrID, cmd.Type, errMsg, resultBlob)
}

// proxyForwardRequest re-sends a forwarded command to the authoritative leader
// on behalf of the original requester. The leader's response envelope already
// carries the original corrID, so when handleForwardResponse fires on this
// member it relays the bytes verbatim back to the requester.
func (s *Service) proxyForwardRequest(originNode pid.NodeID, corrID uint64, hop uint8, data []byte, next pid.NodeID) {
	// Reserve a proxy slot on the original corrID so the leader's reply
	// (which arrives on this node, not the original requester) is intercepted
	// and relayed onward instead of being delivered to a local waiter.
	s.installForwardProxy(corrID, originNode)

	envelope := encodeForwardRequest(corrID, hop, data)
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		next, HostID,
		topicForwardRequest,
		payload.New(envelope),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.removeForwardProxy(corrID)
		s.replyForward(originNode, corrID, 0, err.Error(), nil)
	}
}

// replyForward sends the v1 typed envelope back to the requesting follower.
func (s *Service) replyForward(sourceNode pid.NodeID, corrID uint64, cmd CommandType, errMsg string, result []byte) {
	respEnvelope, err := encodeForwardResponse(corrID, cmd, errMsg, result)
	if err != nil {
		s.logger.Error("encode forward response envelope",
			zap.Error(err), zap.String("to", sourceNode))
		return
	}

	respPkg := relay.NewServicePackage(
		s.localNode, HostID,
		sourceNode, HostID,
		topicForwardResponse,
		payload.New(respEnvelope),
	)

	if err := s.router.Send(respPkg); err != nil {
		relay.ReleasePackage(respPkg)
		s.logger.Error("failed to send forward response",
			zap.Error(err), zap.String("to", sourceNode))
	}
}

// handleForwardResponse processes a response from the leader for a forwarded
// command. When this node is an intermediate hop (its corrID is registered in
// forwardProxies because it re-forwarded the original request on behalf of a
// non-member), the response bytes are relayed verbatim to the origin node
// instead of being delivered locally. Otherwise it accepts both legacy v0
// (error-string-only) and new v1 (typed result) envelopes so a mid-upgrade
// cluster never wedges a follower.
func (s *Service) handleForwardResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward response payload")
		return
	}

	corrID := binary.BigEndian.Uint64(envelope[:8])
	if origin, isProxy := s.takeForwardProxy(corrID); isProxy {
		s.relayForwardResponse(origin, envelope)
		return
	}

	decoded, err := decodeForwardResponse(envelope)
	if err != nil {
		s.logger.Error("failed to decode forward response envelope",
			zap.Error(err), zap.Uint64("corr_id", decoded.CorrID))
		s.deliverForward(decoded.CorrID, &forwardResponse{
			ErrMsg: fmt.Sprintf("decode forward response: %v", err),
		})
		return
	}

	resp := &forwardResponse{
		ErrMsg: decoded.ErrMsg,
		Result: decoded.Result,
		V1:     decoded.V1,
	}
	s.deliverForward(decoded.CorrID, resp)
}

// installForwardProxy records that this node is the intermediate hop for the
// given corrID; the original request came from origin. handleForwardResponse
// uses this map to redirect the leader's reply.
func (s *Service) installForwardProxy(corrID uint64, origin pid.NodeID) {
	s.mu.Lock()
	s.forwardProxies[corrID] = origin
	s.mu.Unlock()
}

// removeForwardProxy clears a proxy entry without consuming a reply. Used on
// send failure when no leader reply will arrive.
func (s *Service) removeForwardProxy(corrID uint64) {
	s.mu.Lock()
	delete(s.forwardProxies, corrID)
	s.mu.Unlock()
}

// takeForwardProxy atomically reads and removes a proxy entry. Returns the
// origin and true if a proxy was registered for this corrID, else ("", false).
func (s *Service) takeForwardProxy(corrID uint64) (pid.NodeID, bool) {
	s.mu.Lock()
	origin, ok := s.forwardProxies[corrID]
	if ok {
		delete(s.forwardProxies, corrID)
	}
	s.mu.Unlock()
	return origin, ok
}

// relayForwardResponse forwards a leader-emitted response envelope verbatim to
// the original requester. The envelope already carries the right corrID, so
// the origin's pending waiter resolves without this node needing to decode.
func (s *Service) relayForwardResponse(origin pid.NodeID, envelope []byte) {
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		origin, HostID,
		topicForwardResponse,
		payload.New(envelope),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg: relay forward response failed",
			zap.String("to", origin), zap.Error(err))
	}
}

// deliverForward delivers a parsed response to the waiting forwardToLeader
// goroutine, if any. Lost responses (no pending entry) are logged at debug
// level — the only callers are the timeout / stop branches in forwardToLeader.
func (s *Service) deliverForward(corrID uint64, resp *forwardResponse) {
	s.mu.Lock()
	ch, ok := s.pending[corrID]
	s.mu.Unlock()
	if !ok {
		s.logger.Debug("received forward response for unknown correlation ID",
			zap.Uint64("corr_id", corrID))
		return
	}
	select {
	case ch <- resp:
	default:
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
	_ globalreg.Registry = (*Service)(nil)
	_ relay.Receiver     = (*Service)(nil)
)
