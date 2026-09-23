// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"
	"go.uber.org/zap"
)

// STRONG-scope reservation plane: leader-quorum name reservations with
// per-node observations, ack collection, expiry timers, and the pending
// rebroadcast/nudge machinery. Split out of service.go; same package.

// strongOutcome is the value the Register caller blocks on while waiting for
// the FSM to commit Active or Expired.
type strongOutcome struct {
	MissingAcks []pid.NodeID
	Epoch       uint64
	State       global.RegisterState
}

// observationState tracks where a held Strong observation sits in its lifecycle.
type observationState uint8

const (
	// observationPending records a Strong attempt acknowledged by this node.
	observationPending observationState = iota
	// observationActive is the converted state after the name promotes to active.
	// The observation persists until the matching global binding is removed.
	observationActive
)

// strongObservation is a node-local attestation that this node acked a Strong
// reservation for a name at a given epoch. The
// epoch is the pending's Raft epoch — the instance id. The observation is installed
// Pending on ack, converted Pending->Active on promotion, and released only on a
// committed terminal FSM event matching the held epoch (indexed release).
type strongObservation struct {
	pid   pid.PID
	epoch uint64
	state observationState
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

// releaseEnvelope is the wire form of a leader → observation-holder release
// (topicReleaseObservation). The recipient releases its Strong observation for the
// carried (name, epoch) idempotently. The epoch makes the release indexed: a
// stale release for an older instance never clears a newer same-name observation.
type releaseEnvelope struct {
	Name  string `codec:"n"`
	Epoch uint64 `codec:"e"`
}

func (s *Service) registerStrong(ctx context.Context, name string, p pid.PID) (global.RegisterOutcome, error) {
	if s.membership == nil {
		return global.RegisterOutcome{}, fmt.Errorf("globalreg: Strong scope requires cluster membership")
	}

	nodeID := p.Node
	if nodeID == "" {
		nodeID = s.localNode
	}

	deadline := time.Now().Add(global.StrongDeadline)
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
		return global.RegisterOutcome{}, err
	}

	result, ok := resp.(*RegisterResult)
	if !ok {
		return global.RegisterOutcome{}, fmt.Errorf("unexpected pending response type: %T", resp)
	}
	if result.Err != nil {
		return global.RegisterOutcome{
			ExistingPID: result.ExistingPID,
		}, result.Err
	}

	epoch := result.FenceToken
	s.rebindStrongWatcher(name, 0, epoch)

	s.monitorPID(p)

	for {
		select {
		case <-ctx.Done():
			return global.RegisterOutcome{Epoch: epoch}, ctx.Err()
		case <-s.stopCh:
			return global.RegisterOutcome{Epoch: epoch}, global.ErrNotAvailable
		case outcome := <-watchCh:
			switch outcome.State {
			case global.RegisterStateActive:
				return global.RegisterOutcome{
					PID:   p,
					Epoch: outcome.Epoch,
					State: global.RegisterStateActive,
				}, nil
			case global.RegisterStateExpired:
				missing := make([]string, len(outcome.MissingAcks))
				copy(missing, outcome.MissingAcks)
				result := global.RegisterOutcome{Epoch: outcome.Epoch, State: global.RegisterStateExpired}
				return result, &global.StrongRegistrationTimeoutError{
					Name:        name,
					Epoch:       outcome.Epoch,
					MissingAcks: missing,
				}
			}
		}
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
// A required node acknowledges its observation of this Strong attempt. The
// leader applies its ack directly (asynchronously, since Apply is
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

// evaluatePending records this replica's observation before acknowledging the
// Strong attempt. LOCAL and EVENTUAL names are independent of this namespace.
func (s *Service) evaluatePending(name string, epoch uint64, pendingPID pid.PID) {
	s.latchReservation(name, pendingPID, epoch)
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		s.applySelfAck(name, epoch)
		return
	}
	if err := s.sendAck(name, epoch); err != nil {
		s.logger.Debug("globalreg: send strong ack failed",
			zap.String("name", name), zap.Uint64("epoch", epoch), zap.Error(err))
	}
}

// latchReservation remembers the observed Strong attempt for snapshot and
// terminal-event reconciliation. It does not reserve names in other scopes.
func (s *Service) latchReservation(name string, pendingPID pid.PID, epoch uint64) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	if existing, ok := s.strongObservations[name]; ok && existing.epoch == epoch {
		return
	}
	s.strongObservations[name] = strongObservation{pid: pendingPID, epoch: epoch, state: observationPending}
}

// promoteObservation converts the observed reservation from Pending to Active for
// the matching (name, epoch). A mismatched epoch is a no-op so a
// stale activation never disturbs a newer instance.
func (s *Service) promoteObservation(name string, epoch uint64) {
	s.reserveMu.Lock()
	if e, ok := s.strongObservations[name]; ok && e.epoch == epoch {
		e.state = observationActive
		s.strongObservations[name] = e
	}
	s.reserveMu.Unlock()
}

// releaseObservation drops a held observation for name only when its epoch matches
// the terminal event's. A mismatched epoch leaves a newer observation intact
// (indexed release). Released on a committed terminal FSM event or a leader
// release delivery — never on a local timeout.
func (s *Service) releaseObservation(name string, epoch uint64) {
	s.reserveMu.Lock()
	if e, ok := s.strongObservations[name]; ok && e.epoch == epoch {
		delete(s.strongObservations, name)
	}
	s.reserveMu.Unlock()
}

// isStrongReserved reports a held observation for name in EITHER state
// (test/internal read).
func (s *Service) isStrongReserved(name string) (pid.PID, bool) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	e, ok := s.strongObservations[name]
	return e.pid, ok
}

// IsStrongReserved reports whether this node observed a Strong reservation for name
// in either the Pending (acked, awaiting promotion) or Active (promoted)
// state. The result describes global ownership only; other naming scopes keep
// independent bindings and do not use this observation as an admission veto.
func (s *Service) IsStrongReserved(name string) (pid.PID, bool) {
	if s == nil {
		return pid.PID{}, false
	}
	return s.isStrongReserved(name)
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
// converts the held observation Pending->Active so it persists through promotion.
// The outcome carries the activation Raft index (ev.ActivationIdx) as the
// registration epoch. Promotion is NOT a terminal — the observation is kept.
func (s *Service) handleActiveEvent(ev ActiveEvent) {
	s.stopStrongTimer(ev.Name, ev.Epoch)
	s.promoteObservation(ev.Name, ev.Epoch)
	s.deliverStrongOutcome(ev.Name, ev.Epoch, strongOutcome{
		State: global.RegisterStateActive,
		Epoch: ev.ActivationIdx,
	})
}

// handleExpiredEvent wakes any local Register caller waiting on this entry and
// releases the held observation (indexed to ev.Epoch). This is the single terminal
// path for expire, reject, unreserve, pid-exit, and an active name being
// unregistered. The leader also delivers a release to RequiredNodes so each
// replica drops the matching observation.
func (s *Service) handleExpiredEvent(ev ExpiredEvent) {
	s.stopStrongTimer(ev.Name, ev.Epoch)
	s.releaseObservation(ev.Name, ev.Epoch)
	s.deliverReleaseToHolders(ev.Name, ev.Epoch, ev.RequiredNodes)
	s.deliverStrongOutcome(ev.Name, ev.Epoch, strongOutcome{
		State:       global.RegisterStateExpired,
		Epoch:       ev.Epoch,
		MissingAcks: ev.MissingAcks,
	})
}

// deliverReleaseToHolders sends a targeted observation release to every holder of
// the named Strong reservation. The leader knows the holders (RequiredNodes);
// the local node releases via handleExpiredEvent's own releaseObservation, so a
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
		if err := s.sendReleaseObservation(h, name, epoch); err != nil {
			s.logger.Debug("globalreg: release observation send failed",
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
	// the pending via the snapshot and installs the observation instead.
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

// sendReleaseObservation sends a targeted release to a node holding a Strong
// observation for (name, epoch). The recipient releases its observation idempotently.
// Does not touch Raft.
func (s *Service) sendReleaseObservation(targetNode pid.NodeID, name string, epoch uint64) error {
	body, err := marshalMsgpack(releaseEnvelope{Name: name, Epoch: epoch})
	if err != nil {
		return err
	}
	pkg := relay.NewServicePackage(
		s.localNode, HostID,
		targetNode, HostID,
		topicReleaseObservation,
		payload.New(body),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	return nil
}

// handleReleaseObservation processes a leader release delivery. The node releases
// its held Strong observation for the carried (name, epoch). The release is
// indexed: a stale release for an older instance leaves a newer same-name
// observation intact. Idempotent — releasing a name this node never held is a
// no-op.
func (s *Service) handleReleaseObservation(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env releaseEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed observation release", zap.Error(err))
		return
	}
	s.releaseObservation(env.Name, env.Epoch)
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
