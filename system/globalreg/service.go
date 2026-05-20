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
	// topicRegisterAck delivers a Root-scope ack from a follower to the
	// current leader. Body is a msgpack-encoded ackEnvelope.
	topicRegisterAck relay.Topic = "globalreg.root.ack"

	defaultApplyTimeout = 10 * time.Second

	// rootRebroadcastDivisor controls how often the leader re-broadcasts
	// pending entries. Set to 4 so a 10s deadline triggers ~2 rebroadcasts.
	rootRebroadcastDivisor = 4
)

// Service implements the globalreg.Registry interface.
// It wraps a Raft-backed FSM and provides leader forwarding for writes
// and topology-based auto-cleanup.
type Service struct {
	monitoredPIDs    sync.Map
	router           relay.Receiver
	raftSvc          raftapi.Service
	bus              event.Bus
	topo             topology.Topology
	membership       cluster.Membership
	stopCh           chan struct{}
	logger           *zap.Logger
	fsm              *FSM
	tel              *telemetry
	pending          map[uint64]chan *forwardResponse
	rootWatchers     map[string]map[uint64]chan rootOutcome
	rootTimers       map[string]*rootTimer
	rebroadcastStop  chan struct{}
	localNode        pid.NodeID
	mu               sync.Mutex
	rootMu           sync.Mutex
	monitorWatermark atomic.Uint64
	started          bool
	ready            bool
	degraded         bool
}

// rootOutcome is the value the Register caller blocks on while waiting for
// the FSM to commit Active or Expired.
type rootOutcome struct {
	MissingAcks []pid.NodeID
	Epoch       uint64
	State       globalreg.RegisterState
}

// rootTimer is the leader-side deadline goroutine for a pending Root entry.
type rootTimer struct {
	deadline time.Time
	stop     chan struct{}
	name     string
	epoch    uint64
}

// ackEnvelope is the wire form of a Root-scope ack delivered to the leader
// over the relay (topicRegisterAck).
type ackEnvelope struct {
	Name      string     `codec:"n"`
	AckerNode pid.NodeID `codec:"a"`
	Epoch     uint64     `codec:"e"`
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
// membership may be nil for unit tests that don't exercise Root scope —
// any Root-scope register will then fail loudly.
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
		raftSvc:      raftSvc,
		fsm:          fsm,
		tel:          tel,
		bus:          bus,
		topo:         topo,
		router:       router,
		membership:   membership,
		localNode:    localNode,
		logger:       logger,
		stopCh:       make(chan struct{}),
		pending:      make(map[uint64]chan *forwardResponse),
		rootWatchers: make(map[string]map[uint64]chan rootOutcome),
		rootTimers:   make(map[string]*rootTimer),
	}
	if fsm != nil {
		fsm.SetOnRestore(s.resetMonitorWatermark)
		fsm.SetOnPending(s.handlePendingEvent)
		fsm.SetOnActive(s.handleActiveEvent)
		fsm.SetOnExpired(s.handleExpiredEvent)
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

	ch := make(chan event.Event, 32)
	subID, err := s.bus.SubscribeP(ctx, cluster.System, cluster.NodeLeft, ch)
	if err != nil {
		return nil, fmt.Errorf("subscribe to cluster events: %w", err)
	}
	go s.handleClusterEvents(ctx, ch, subID)

	go s.monitorLeadership()

	go s.waitForReady(ctx)

	go s.rebroadcastLoop()

	statusCh := make(chan any, 1)
	go func() {
		defer close(statusCh)
		<-s.stopCh
	}()

	s.logger.Info("global registry service started", zap.String("node", s.localNode))
	return statusCh, nil
}

// rebroadcastLoop periodically nudges pending entries on the leader. The
// cadence is keyed to RootDeadline / rootRebroadcastDivisor so a 10s
// deadline produces ~2 nudges before expiry.
func (s *Service) rebroadcastLoop() {
	interval := globalreg.RootDeadline / rootRebroadcastDivisor
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

	if err := s.RemoveNode(ctx, nodeID); err != nil {
		s.logger.Error("failed to remove node names", zap.String("node", nodeID), zap.Error(err))
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
	case globalreg.Root:
		return s.registerRoot(ctx, name, p)
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

func (s *Service) registerRoot(ctx context.Context, name string, p pid.PID) (globalreg.RegisterOutcome, error) {
	if s.membership == nil {
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: Root scope requires cluster membership")
	}

	nodeID := p.Node
	if nodeID == "" {
		nodeID = s.localNode
	}

	required := s.snapshotRequiredNodes()
	if len(required) == 0 {
		return globalreg.RegisterOutcome{}, fmt.Errorf("globalreg: Root register found no live nodes")
	}

	deadline := time.Now().Add(globalreg.RootDeadline)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) && time.Until(ctxDeadline) >= 50*time.Millisecond {
		deadline = ctxDeadline
	}

	watchCh := make(chan rootOutcome, 1)
	s.installRootWatcher(name, 0, watchCh)
	defer s.releaseRootWatcher(name, 0, watchCh)

	cmd := &Command{
		Type:             CmdRegisterPending,
		Name:             name,
		PID:              p,
		NodeID:           nodeID,
		RequiredNodes:    required,
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
	s.rebindRootWatcher(name, 0, epoch)

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
				missing := make([]string, len(outcome.MissingAcks))
				copy(missing, outcome.MissingAcks)
				return globalreg.RegisterOutcome{
						Epoch: outcome.Epoch,
						State: globalreg.RegisterStateExpired,
					}, &globalreg.RootRegistrationTimeoutError{
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
	case globalreg.Root:
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
// when opening a Root reservation. Includes the local node. Sorted for
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

// installRootWatcher registers a channel that will receive the rootOutcome
// for (name, epoch). epoch=0 is used briefly while the caller waits for the
// FSM to assign an epoch via the pending response.
func (s *Service) installRootWatcher(name string, epoch uint64, ch chan rootOutcome) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	m, ok := s.rootWatchers[name]
	if !ok {
		m = make(map[uint64]chan rootOutcome, 1)
		s.rootWatchers[name] = m
	}
	m[epoch] = ch
}

// rebindRootWatcher moves a watcher channel from one epoch key to another.
// Used after the caller learns the epoch assigned to its pending entry.
func (s *Service) rebindRootWatcher(name string, fromEpoch, toEpoch uint64) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	m, ok := s.rootWatchers[name]
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

func (s *Service) releaseRootWatcher(name string, epoch uint64, ch chan rootOutcome) {
	s.rootMu.Lock()
	defer s.rootMu.Unlock()
	m, ok := s.rootWatchers[name]
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
		delete(s.rootWatchers, name)
	}
}

// deliverRootOutcome wakes any watcher registered for (name, epoch).
func (s *Service) deliverRootOutcome(name string, epoch uint64, outcome rootOutcome) {
	s.rootMu.Lock()
	m, ok := s.rootWatchers[name]
	if !ok {
		s.rootMu.Unlock()
		return
	}
	chans := make([]chan rootOutcome, 0, 2)
	if ch, ok := m[epoch]; ok {
		chans = append(chans, ch)
	}
	if ch, ok := m[0]; ok && epoch != 0 {
		chans = append(chans, ch)
	}
	s.rootMu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- outcome:
		default:
		}
	}
}

// handlePendingEvent runs on every replica from FSM.Apply.
// Followers send an ack to the leader via the relay. The leader bypasses
// the relay and applies the ack directly (asynchronously, since Apply is
// single-threaded and re-entering would deadlock).
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
			s.startRootTimer(ev.Name, ev.Epoch, time.Unix(0, ev.DeadlineUnixNano))
		}
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		s.startRootTimer(ev.Name, ev.Epoch, time.Unix(0, ev.DeadlineUnixNano))
		go s.applySelfAck(ev.Name, ev.Epoch)
		return
	}
	if err := s.sendAck(ev.Name, ev.Epoch); err != nil {
		s.logger.Debug("globalreg: send root ack failed",
			zap.String("name", ev.Name), zap.Uint64("epoch", ev.Epoch), zap.Error(err))
	}
}

// applySelfAck records the leader's own ack via Raft. Runs in a fresh
// goroutine so it does not re-enter FSM.Apply.
func (s *Service) applySelfAck(name string, epoch uint64) {
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

// handleActiveEvent wakes any local Register caller waiting on this entry.
// The outcome carries the activation Raft index (ev.ActivationIdx) as the
// fence token — that matches what Lookup(WithFence) will return after
// the names map is updated.
func (s *Service) handleActiveEvent(ev ActiveEvent) {
	s.stopRootTimer(ev.Name, ev.Epoch)
	s.deliverRootOutcome(ev.Name, ev.Epoch, rootOutcome{
		State: globalreg.RegisterStateActive,
		Epoch: ev.ActivationIdx,
	})
}

// handleExpiredEvent wakes any local Register caller waiting on this entry.
func (s *Service) handleExpiredEvent(ev ExpiredEvent) {
	s.stopRootTimer(ev.Name, ev.Epoch)
	s.deliverRootOutcome(ev.Name, ev.Epoch, rootOutcome{
		State:       globalreg.RegisterStateExpired,
		Epoch:       ev.Epoch,
		MissingAcks: ev.MissingAcks,
	})
}

// sendAck delivers a Root-scope ack to the current leader over the relay.
func (s *Service) sendAck(name string, epoch uint64) error {
	leaderID, _, err := s.raftSvc.Leader()
	if err != nil || leaderID == "" {
		return fmt.Errorf("no leader to ack")
	}
	body, err := marshalMsgpack(ackEnvelope{Name: name, Epoch: epoch, AckerNode: s.localNode})
	if err != nil {
		return err
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		leaderID, HostID,
		topicRegisterAck,
		payload.New(body),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	return nil
}

// startRootTimer is called by the leader to arm a deadline timer for a
// pending entry. Stopping the timer is safe to call from a different
// goroutine; the timer goroutine exits cleanly on stop.
func (s *Service) startRootTimer(name string, epoch uint64, deadline time.Time) {
	key := rootTimerKey(name, epoch)
	stop := make(chan struct{})
	s.rootMu.Lock()
	if existing, ok := s.rootTimers[key]; ok {
		close(existing.stop)
	}
	t := &rootTimer{name: name, epoch: epoch, stop: stop, deadline: deadline}
	s.rootTimers[key] = t
	s.rootMu.Unlock()

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
			s.fireRootExpire(name, epoch, "missing_ack")
		case <-stop:
			return
		case <-s.stopCh:
			return
		}
	}()
}

func (s *Service) stopRootTimer(name string, epoch uint64) {
	key := rootTimerKey(name, epoch)
	s.rootMu.Lock()
	if t, ok := s.rootTimers[key]; ok {
		close(t.stop)
		delete(s.rootTimers, key)
	}
	s.rootMu.Unlock()
}

func (s *Service) fireRootExpire(name string, epoch uint64, reason string) {
	cmd := &Command{Type: CmdRegisterExpired, Name: name, Epoch: epoch, Reason: reason}
	if _, err := s.applyCommand(cmd); err != nil {
		s.logger.Warn("globalreg: fire root expire failed",
			zap.String("name", name), zap.Uint64("epoch", epoch),
			zap.String("reason", reason), zap.Error(err))
	}
}

func rootTimerKey(name string, epoch uint64) string {
	return fmt.Sprintf("%s|%d", name, epoch)
}

// rebroadcastPending re-applies CmdRegisterPending for entries that have
// not yet collected every ack, letting followers retry their ack send if
// the original relay delivery was dropped. Only runs on the leader.
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
				s.handlePendingEvent(PendingEvent{
					Name:             v.Name,
					PID:              v.PID,
					Epoch:            v.Epoch,
					RequiredNodes:    v.RequiredNodes,
					DeadlineUnixNano: v.DeadlineUnixNano,
				})
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

// sendPendingNudge re-instructs a follower that has not acked yet to look
// at its own FSM state and re-emit. Implemented by re-applying through
// CmdRegisterPending — the pendingDedupe path makes this idempotent.
func (s *Service) sendPendingNudge(targetNode pid.NodeID, v PendingView) error {
	cmd := &Command{
		Type:             CmdRegisterPending,
		Name:             v.Name,
		PID:              v.PID,
		NodeID:           v.PID.Node,
		Epoch:            v.Epoch,
		RequiredNodes:    v.RequiredNodes,
		DeadlineUnixNano: v.DeadlineUnixNano,
	}
	_, err := s.applyCommand(cmd)
	_ = targetNode
	return err
}

// Lookup reads the registry from the local Raft FSM replica with optional
// behavior controlled by LookupOptions. See api/globalreg.Registry.Lookup
// for the option semantics. Lookup is lock-free and O(1) for the no-options
// and WithFence paths; ByPID is O(shards) because it scans the reverse index.
func (s *Service) Lookup(_ context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error) {
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

	if o.WithFence {
		s.mu.Lock()
		ready := s.ready
		s.mu.Unlock()
		if !ready {
			return globalreg.LookupResult{}, nil
		}

		p, token, found := state.LookupWithFence(name)
		return globalreg.LookupResult{
			PID:        p,
			FenceToken: token,
			Found:      found,
		}, nil
	}

	p, found := state.Lookup(name)
	return globalreg.LookupResult{PID: p, Found: found}, nil
}

// Deprecated: use Lookup(ctx, name, globalreg.WithFence()).
func (s *Service) LookupWithFence(name string) globalreg.LookupResult {
	r, _ := s.Lookup(context.Background(), name, globalreg.WithFence())
	return r
}

// Deprecated: use globalreg.ValidateFence(ctx, reg, name, token). Kept here
// as a direct FSM-state check so the relay fence-validation hot path does
// not pay the option-parsing overhead.
func (s *Service) ValidateFence(name string, token uint64) error {
	if !s.fsm.State().ValidateFence(name, token) {
		return globalreg.ErrStaleFence
	}
	return nil
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

// applyCommand encodes and proposes a command to Raft.
// If this node is not the leader, the command is forwarded via relay.
func (s *Service) applyCommand(cmd *Command) (any, error) {
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

// forwardToLeader sends the command to the current Raft leader's global
// registry service via the relay system and waits for the response.
// Uses a correlation ID to match the response to the request. On success it
// returns the typed FSM result decoded on the follower side.
func (s *Service) forwardToLeader(cmd *Command) (any, error) {
	start := time.Now()
	// Retry leader discovery - follower may need time to learn leader after joining cluster.
	var leaderID string
	backoff := 100 * time.Millisecond
	for i := 0; i < 30; i++ {
		var err error
		leaderID, _, err = s.raftSvc.Leader()
		if err == nil && leaderID != "" {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
		if backoff > time.Second {
			backoff = time.Second
		}
	}
	if leaderID == "" {
		s.tel.recordForwardedApply(cmd.Type, forwardResultNoLeader, time.Since(start))
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

	// Prepend 8-byte correlation ID to the command data.
	envelope := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(envelope[:8], corrID)
	copy(envelope[8:], data)

	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		leaderID, HostID,
		topicForwardRequest,
		payload.New(envelope),
	)

	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.tel.recordForwardedApply(cmd.Type, forwardResultSendFailed, time.Since(start))
		return nil, fmt.Errorf("forward to leader: %w", err)
	}

	select {
	case resp := <-respCh:
		if resp.ErrMsg != "" {
			s.tel.recordForwardedApply(cmd.Type, forwardResultError, time.Since(start))
			return nil, errors.New(resp.ErrMsg)
		}
		s.tel.recordForwardedApply(cmd.Type, forwardResultOK, time.Since(start))
		return resp.Result, nil
	case <-time.After(defaultApplyTimeout):
		s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
		return nil, globalreg.ErrNotAvailable
	case <-s.stopCh:
		s.tel.recordForwardedApply(cmd.Type, forwardResultTimeout, time.Since(start))
		return nil, globalreg.ErrNotAvailable
	}
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
		case topology.TopicEvents:
			s.handleExitEvent(msg)
		}
	}

	return nil
}

// handleRegisterAck routes an ack arriving over the relay through Raft.
// Only the leader does anything useful — followers receiving an ack
// (shouldn't happen in practice but is harmless) drop it.
func (s *Service) handleRegisterAck(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	if !s.raftSvc.IsLeader() {
		return
	}
	var env ackEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed root ack", zap.Error(err))
		return
	}
	cmd := &Command{
		Type:      CmdRegisterAck,
		Name:      env.Name,
		Epoch:     env.Epoch,
		AckerNode: env.AckerNode,
	}
	if _, err := s.applyCommand(cmd); err != nil {
		s.logger.Debug("globalreg: apply root ack failed",
			zap.String("name", env.Name), zap.Uint64("epoch", env.Epoch), zap.Error(err))
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

// handleForwardRequest processes a forwarded command from a follower node.
// Applies the command via Raft and sends a v1 typed response back with the
// correlation ID. The follower decodes the typed FSM result.
func (s *Service) handleForwardRequest(source pid.PID, msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward request payload")
		return
	}

	corrID := binary.BigEndian.Uint64(envelope[:8])
	data := envelope[8:]

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
// command. It accepts both legacy v0 (error-string-only) and new v1 (typed
// result) envelopes so a mid-upgrade cluster never wedges a follower.
func (s *Service) handleForwardResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}

	envelope, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(envelope) < 8 {
		s.logger.Error("invalid forward response payload")
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

	s.resumeRootTimers()
}

// resumeRootTimers walks pending Root entries and arms a deadline timer
// for each one this node now owns as leader. Called on leader transition.
func (s *Service) resumeRootTimers() {
	if s.fsm == nil {
		return
	}
	for _, v := range s.fsm.State().listPending() {
		s.startRootTimer(v.Name, v.Epoch, time.Unix(0, v.DeadlineUnixNano))
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

// PendingSnapshot returns the current Root-scope pending reservations.
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

// RecordFenceRejection emits the pg_fence_rejection_total counter. It is
// intended to be wired as a callback from the relay Router so the rejection
// site (which validates the fence token before delivery) does not have to
// depend on the metrics package directly.
func (s *Service) RecordFenceRejection(name, reason string) {
	if s == nil || s.tel == nil {
		return
	}

	// Use the global name as the "pg" label; for pg-process-group messages
	// the global name is the group identifier, which is exactly what the
	// dashboard wants to slice by.
	pg := name
	if pg == "" {
		pg = HostID
	}

	s.tel.recordFenceRejection(pg, reason)

	// Emit a single info-level log line per rejection so chaos rigs and
	// ops dashboards (which today have no scrape endpoint inside the
	// runtime binary) can correlate stale-fence drops with the global
	// name and reason without having to mount Prometheus.
	if s.logger != nil {
		s.logger.Info("globalreg fence rejection",
			zap.String("pg", pg),
			zap.String("reason", reason))
	}
}

// Ensure Service implements the interfaces.
var (
	_ globalreg.Registry = (*Service)(nil)
	_ relay.Receiver     = (*Service)(nil)
)
