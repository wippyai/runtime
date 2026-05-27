// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"time"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"

	"go.uber.org/zap"
)

const (
	// defaultLeaderProbeInterval is the cadence of the leader-reachability probe.
	defaultLeaderProbeInterval = 3 * time.Second
	// defaultLeaderProbeGrace is how many consecutive probe failures must occur
	// before the monitor declares the leader unreachable and closes the gate. A
	// single transient failure within the grace does not close it.
	defaultLeaderProbeGrace = 3
	// leaderPingTimeout bounds a single leader-ping round-trip.
	leaderPingTimeout = 2 * time.Second
)

// leaderPingEnvelope is the wire form of a leader-reachability probe
// (topicLeaderPing). CorrID matches the leader's pong reply to the waiting
// prober. The probe never carries state — a successful round-trip is the
// only signal. Hop counts re-forward hops: a non-leader member receiving a
// ping re-forwards once to its authoritative Leader() (leaving NodeID
// untouched so the pong reaches the original prober directly).
type leaderPingEnvelope struct {
	NodeID pid.NodeID `codec:"nd"`
	CorrID uint64     `codec:"c"`
	Hop    uint8      `codec:"h,omitempty"`
}

// leaderPongEnvelope is the leader's reply to a leader-ping.
type leaderPongEnvelope struct {
	CorrID uint64 `codec:"c"`
}

// monitorLeaderReachability ties name-readiness to leader reachability rather
// than peer membership churn. It probes the leader on a fixed cadence and runs
// a debounced state machine:
//
//   - reachable -> unreachable (after probeGrace consecutive failures): close
//     the name-ready gate. A partitioned node can no longer be sure it isn't
//     missing strong updates the leader committed (a CmdDropRequired drop and
//     promote can land without its ack), so it must stop serving LOCAL/EVENTUAL
//     names until it re-barriers.
//   - unreachable -> reachable (first success after a loss): run the rejoin
//     barrier (epoch bump + snapshot fetch + conflict revoke); the gate reopens
//     only when the barrier completes.
//
// A node that is itself the leader reaches itself trivially, so it stays
// reachable and its Start barrier covers it. The initial state is reachable so
// the first-join path is owned by joinBarrierOnStart, not a spurious rejoin.
func (s *Service) monitorLeaderReachability() {
	interval := s.probeInterval
	if interval <= 0 {
		interval = defaultLeaderProbeInterval
	}
	grace := s.probeGrace
	if grace <= 0 {
		grace = defaultLeaderProbeGrace
	}

	t := time.NewTicker(interval)
	defer t.Stop()

	reachable := true
	failures := 0
	for {
		select {
		case <-s.stopCh:
			return
		case <-t.C:
			if s.pingLeader() == nil {
				failures = 0
				if !reachable {
					reachable = true
					s.logger.Info("globalreg: leader reachability recovered, re-running join barrier",
						zap.String("node", s.localNode))
					s.triggerRejoinBarrier()
				}
				continue
			}
			failures++
			if reachable && failures >= grace {
				reachable = false
				s.nameReady.Store(false)
				s.logger.Warn("globalreg: leader unreachable past grace, closing name-ready gate",
					zap.String("node", s.localNode), zap.Int("failures", failures))
			}
		}
	}
}

// pingLeader probes leader reachability. The leader reaches itself trivially.
// A follower forwards directly to the known leader (1 hop). A non-member —
// which never observes the leader through Leader() — sends to a deterministic
// raft member instead; if that member is the leader it replies success
// immediately, otherwise it re-forwards once to its authoritative Leader()
// and the leader's pong reaches the original prober directly (the pong's
// destination is the prober's NodeID). A successful round-trip attests the
// leader is reachable. Tries candidates in order: any candidate that yields a
// pong is sufficient.
func (s *Service) pingLeader() error {
	if s.raftSvc == nil {
		return globalreg.ErrNotAvailable
	}
	if s.raftSvc.IsLeader() {
		return nil
	}

	targets, err := s.resolveForwardTarget()
	if err != nil || len(targets) == 0 {
		return globalreg.ErrNotAvailable
	}

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan struct{}, 1)
	s.joinMu.Lock()
	s.pingPending[corrID] = respCh
	s.joinMu.Unlock()
	defer func() {
		s.joinMu.Lock()
		delete(s.pingPending, corrID)
		s.joinMu.Unlock()
	}()

	body, err := marshalMsgpack(leaderPingEnvelope{NodeID: s.localNode, CorrID: corrID})
	if err != nil {
		return err
	}

	attempts := len(targets)
	if attempts > 3 {
		attempts = 3
	}
	perAttempt := leaderPingTimeout / time.Duration(attempts)
	if perAttempt < 250*time.Millisecond {
		perAttempt = 250 * time.Millisecond
	}

	for i, target := range targets {
		if i >= attempts {
			break
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicLeaderPing,
			payload.New(body),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			continue
		}
		select {
		case <-respCh:
			return nil
		case <-time.After(perAttempt):
			continue
		case <-s.stopCh:
			return globalreg.ErrNotAvailable
		}
	}
	return globalreg.ErrNotAvailable
}

// handleLeaderPing serves a reachability probe. The leader echoes the corrID
// back on topicLeaderPong directly to the original prober (env.NodeID). A
// non-leader member acting as the shared write plane for a non-member
// re-forwards the ping once to its authoritative Leader() — leaving NodeID
// untouched so the pong reaches the original prober without a second hop.
func (s *Service) handleLeaderPing(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env leaderPingEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed leader ping", zap.Error(err))
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		respBody, err := marshalMsgpack(leaderPongEnvelope{CorrID: env.CorrID})
		if err != nil {
			s.logger.Warn("globalreg: encode leader pong", zap.Error(err))
			return
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			env.NodeID, HostID,
			topicLeaderPong,
			payload.New(respBody),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			s.logger.Debug("globalreg: send leader pong failed",
				zap.String("to", env.NodeID), zap.Error(err))
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
		topicLeaderPing,
		payload.New(relayBody),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg: re-forward leader ping failed",
			zap.String("to", next), zap.Error(err))
	}
}

// handleLeaderPong wakes the waiting pingLeader goroutine, if any.
func (s *Service) handleLeaderPong(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env leaderPongEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed leader pong", zap.Error(err))
		return
	}
	s.joinMu.Lock()
	ch, ok := s.pingPending[env.CorrID]
	s.joinMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}
