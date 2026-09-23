// SPDX-License-Identifier: MPL-2.0

package global

import (
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"

	"go.uber.org/zap"
)

const (
	// joinSnapshotStatePending marks a snapshot entry that is a PENDING Strong
	// reservation; the joining node installs an observationPending for it.
	joinSnapshotStatePending uint8 = 0
	// joinSnapshotStateActive marks a snapshot entry that is a promoted (ACTIVE)
	// Strong name; the joining node installs an observationActive for it.
	joinSnapshotStateActive uint8 = 1
	// joinSnapshotStateConsistent marks an ACTIVE CONSISTENT-scope binding.
	// The joining node seeds it into the dissem cache (no observation to install
	// — CONSISTENT names do not participate in the strong observation table).
	joinSnapshotStateConsistent uint8 = 2

	// joinBarrierTimeout bounds a single JoinNameEpoch round-trip.
	joinBarrierTimeout = 10 * time.Second
)

// joinRequestEnvelope is the wire form of a JoinNameEpoch request (topicJoinRequest).
// CorrID matches the leader's reply to the waiting caller; NodeEpoch is the
// requester's current node epoch (carried for diagnostics — the snapshot itself
// does not depend on it). Hop counts re-forward hops so a non-leader member that
// receives a join request re-forwards once to its authoritative Leader() and
// relays the response back to the original requester.
type joinRequestEnvelope struct {
	NodeID    pid.NodeID `codec:"nd"`
	Origin    pid.NodeID `codec:"o,omitempty"`
	CorrID    uint64     `codec:"c"`
	NodeEpoch uint64     `codec:"ne"`
	Hop       uint8      `codec:"h,omitempty"`
}

// joinEntryEnvelope is one PENDING or ACTIVE Strong name in a join snapshot.
type joinEntryEnvelope struct {
	Name  string  `codec:"n"`
	Owner pid.PID `codec:"o"`
	Epoch uint64  `codec:"e"`
	State uint8   `codec:"s"`
}

// joinResponseEnvelope is the leader's reply to a JoinNameEpoch request. Entries
// is the full PENDING∪ACTIVE Strong name set as of StrongIndex (the commit index
// the snapshot was linearized against).
type joinResponseEnvelope struct {
	Entries     []joinEntryEnvelope `codec:"en"`
	CorrID      uint64              `codec:"c"`
	StrongIndex uint64              `codec:"si"`
}

// JoinNameEpoch requests the leader's PENDING∪ACTIVE Strong-name snapshot for
// this node. If this node is the leader it snapshots directly; otherwise it
// forwards to the leader over the relay. The snapshot is linearized against the
// current commit index: pending opens serialize through Raft Apply, so listing
// PENDING∪ACTIVE under the state read-locks at a single commit index yields a
// torn-free set (a concurrently-admitted name is either fully in PENDING or, if
// it just promoted, fully in ACTIVE — never split).
func (s *Service) JoinNameEpoch(nodeEpoch uint64) (*joinResponseEnvelope, error) {
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		return s.buildJoinSnapshot(0), nil
	}
	return s.forwardJoinRequest(nodeEpoch)
}

// buildJoinSnapshot captures the full non-terminal Strong namespace as of the
// current commit index. corrID is stamped into the reply so the leader-side
// handler can address the response; a local (leader) snapshot passes 0.
func (s *Service) buildJoinSnapshot(corrID uint64) *joinResponseEnvelope {
	resp := &joinResponseEnvelope{CorrID: corrID}
	if s.fsm == nil {
		return resp
	}
	if s.raftSvc != nil {
		resp.StrongIndex = s.raftSvc.CommitIndex()
	}
	for _, pv := range s.fsm.State().listPending() {
		resp.Entries = append(resp.Entries, joinEntryEnvelope{
			Name:  pv.Name,
			Owner: pv.PID,
			Epoch: pv.Epoch,
			State: joinSnapshotStatePending,
		})
	}
	for _, av := range s.fsm.State().listActiveStrong() {
		resp.Entries = append(resp.Entries, joinEntryEnvelope{
			Name:  av.Name,
			Owner: av.PID,
			Epoch: av.Epoch,
			State: joinSnapshotStateActive,
		})
	}
	// Extend with active CONSISTENT bindings so the joining node seeds its
	// dissem cache. Without these, a non-member's Lookup for a pre-existing
	// CONSISTENT name resolves only after the first cold-miss forward-resolve.
	s.appendConsistentEntries(resp)
	return resp
}

// forwardJoinRequest sends a JoinNameEpoch request through the leader-directed
// write plane and waits for the snapshot reply. Discovers candidates via
// resolveForwardTarget so a non-member (which never observes the leader
// directly) can still pull the snapshot through any raft member.
func (s *Service) forwardJoinRequest(nodeEpoch uint64) (*joinResponseEnvelope, error) {
	targets, err := s.waitForForwardTargets()
	if err != nil {
		return nil, err
	}

	corrID := correlationIDCounter.Add(1)
	respCh := make(chan *joinResponseEnvelope, 1)
	s.joinMu.Lock()
	s.joinPending[corrID] = respCh
	s.joinMu.Unlock()
	defer func() {
		s.joinMu.Lock()
		delete(s.joinPending, corrID)
		s.joinMu.Unlock()
	}()

	attempts := len(targets)
	if attempts > 3 {
		attempts = 3
	}
	perAttempt := joinBarrierTimeout / time.Duration(attempts)
	if perAttempt < time.Second {
		perAttempt = time.Second
	}

	body, err := marshalMsgpack(joinRequestEnvelope{NodeID: s.localNode, NodeEpoch: nodeEpoch, CorrID: corrID})
	if err != nil {
		return nil, err
	}
	var sendErr error
	for i, target := range targets {
		if i >= attempts {
			break
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			target, HostID,
			topicJoinRequest,
			payload.New(body),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			sendErr = err
			continue
		}
		select {
		case resp := <-respCh:
			return resp, nil
		case <-time.After(perAttempt):
			sendErr = global.ErrNotAvailable
			continue
		case <-s.stopCh:
			return nil, global.ErrNotAvailable
		}
	}
	if sendErr != nil {
		return nil, sendErr
	}
	return nil, global.ErrNotAvailable
}

// handleJoinRequest serves a JoinNameEpoch. The leader builds the snapshot and
// replies on topicJoinResponse to the original requester (env.NodeID). A
// non-leader member acting as the shared write plane for a non-member
// re-forwards the request once to its authoritative Leader() — leaving
// env.NodeID untouched so the leader replies directly to the original
// requester, bypassing the proxy hop for the response.
func (s *Service) handleJoinRequest(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env joinRequestEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed join request", zap.Error(err))
		return
	}
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		resp := s.buildJoinSnapshot(env.CorrID)
		respBody, err := marshalMsgpack(resp)
		if err != nil {
			s.logger.Warn("globalreg: encode join snapshot", zap.Error(err))
			return
		}
		pkg := relay.NewServicePackage(
			s.localNode, HostID,
			env.NodeID, HostID,
			topicJoinResponse,
			payload.New(respBody),
		)
		if err := s.router.Send(pkg); err != nil {
			relay.ReleasePackage(pkg)
			s.logger.Debug("globalreg: send join snapshot failed",
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
		topicJoinRequest,
		payload.New(relayBody),
	)
	if err := s.router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		s.logger.Debug("globalreg: re-forward join request failed",
			zap.String("to", next), zap.Error(err))
	}
}

// handleJoinResponse delivers a leader snapshot to the waiting forwardJoinRequest
// goroutine, if any.
func (s *Service) handleJoinResponse(msg *relay.Message) {
	if len(msg.Payloads) == 0 {
		return
	}
	body, ok := msg.Payloads[0].Data().([]byte)
	if !ok || len(body) == 0 {
		return
	}
	var env joinResponseEnvelope
	if err := unmarshalMsgpack(body, &env); err != nil {
		s.logger.Warn("globalreg: malformed join snapshot", zap.Error(err))
		return
	}
	s.joinMu.Lock()
	ch, ok := s.joinPending[env.CorrID]
	s.joinMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- &env:
	default:
	}
}

// runJoinBarrier executes the join-epoch barrier for the given epoch: fetch the
// leader's PENDING∪ACTIVE Strong snapshot, record each reservation, then flip
// name_ready — but only if epoch is still the current node epoch
// (a newer rejoin trigger aborts this barrier so the old epoch never flips
// ready). Re-running installs the same epoch-keyed observations idempotently;
// LOCAL and EVENTUAL bindings are independent and remain untouched.
func (s *Service) runJoinBarrier(epoch uint64) error {
	snap, err := s.JoinNameEpoch(epoch)
	if err != nil {
		return err
	}
	if s.nodeEpoch.Load() != epoch {
		return nil
	}

	for _, e := range snap.Entries {
		switch e.State {
		case joinSnapshotStateConsistent:
			// CONSISTENT entries do not participate in the strong observation
			// table; they seed the dissem cache only. The seed happens via
			// seedDissemFromSnapshot below.
			continue
		case joinSnapshotStateActive:
			s.installSnapshotObservation(e.Name, e.Owner, e.Epoch, observationActive)
		default:
			s.installSnapshotObservation(e.Name, e.Owner, e.Epoch, observationPending)
		}
	}

	// Seed the dissem cache with ACTIVE entries (STRONG + CONSISTENT) from the
	// snapshot. PENDING entries are skipped — the cache holds only ACTIVE
	// bindings (a Lookup for a pending name resolves only after promotion).
	s.seedDissemFromSnapshot(snap)

	// Only flip ready if no newer rejoin started while the barrier ran and the
	// leader is still reachable (the snapshot fetch above already proved a leader
	// answered, but a leadership flip mid-barrier is benign — the observations are
	// installed regardless and a follow-up rejoin barrier reconverges).
	if s.nodeEpoch.Load() != epoch {
		return nil
	}
	s.nameReady.Store(true)
	s.logger.Info("globalreg: join-epoch barrier complete",
		zap.String("node", s.localNode),
		zap.Uint64("node_epoch", epoch),
		zap.Uint64("strong_index", snap.StrongIndex),
		zap.Int("strong_names", len(snap.Entries)))
	return nil
}

// installSnapshotObservation latches an observation for a snapshot Strong name. It
// installs only when no observation at a newer epoch already holds the name, so a
// re-run or a concurrently-latched live pending is never clobbered by a stale
// snapshot. Owner is the reserving pid; a same-name same-epoch observation is left
// as-is.
func (s *Service) installSnapshotObservation(name string, owner pid.PID, epoch uint64, state observationState) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	if e, ok := s.strongObservations[name]; ok && e.epoch >= epoch {
		return
	}
	s.strongObservations[name] = strongObservation{pid: owner, epoch: epoch, state: state}
}

// joinBarrierOnStart runs the first-join barrier behind Raft readiness. It waits
// for the Raft barrier (so a member node has caught up and the snapshot it may
// serve is current) then runs the join barrier for the current node epoch. A
// non-member node (empty FSM) still gets the leader's snapshot over the relay.
func (s *Service) joinBarrierOnStart() {
	if s.raftSvc != nil {
		_ = s.raftSvc.Barrier(joinBarrierTimeout)
	}
	s.attemptJoinBarrier()
}

// attemptJoinBarrier runs the barrier for the current node epoch, retrying on a
// transient failure (no leader yet) until it completes or the service stops or a
// newer epoch supersedes this attempt.
func (s *Service) attemptJoinBarrier() {
	epoch := s.nodeEpoch.Load()
	backoff := 200 * time.Millisecond
	for {
		if s.nodeEpoch.Load() != epoch {
			return
		}
		if err := s.runJoinBarrier(epoch); err == nil {
			return
		}
		select {
		case <-s.stopCh:
			return
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

// triggerRejoinBarrier bumps the node epoch, closes the name-ready gate, and
// re-runs the barrier. First-join and rejoin share runJoinBarrier; only the
// epoch bump differs. The epoch bump aborts any in-flight barrier for the prior
// epoch (it will not flip ready). Idempotent across repeated triggers. Invoked
// when leader reachability recovers after a loss (monitorLeaderReachability).
func (s *Service) triggerRejoinBarrier() {
	s.nodeEpoch.Add(1)
	s.nameReady.Store(false)
	go s.attemptJoinBarrier()
}
