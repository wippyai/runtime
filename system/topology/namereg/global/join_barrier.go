// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology/namereg/global"

	"go.uber.org/zap"
)

const (
	// joinSnapshotStatePending marks a snapshot entry that is a PENDING Strong
	// reservation; the joining node installs an exclusionPending for it.
	joinSnapshotStatePending uint8 = 0
	// joinSnapshotStateActive marks a snapshot entry that is a promoted (ACTIVE)
	// Strong name; the joining node installs an exclusionActive for it.
	joinSnapshotStateActive uint8 = 1
	// joinSnapshotStateConsistent marks an ACTIVE CONSISTENT-scope binding.
	// The joining node seeds it into the dissem cache (no exclusion to install
	// — CONSISTENT names do not participate in the strong exclusion table).
	joinSnapshotStateConsistent uint8 = 2
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

// joinResponseEnvelope is the leader's reply. Entries contains the complete
// registration state at StrongIndex, the captured FSM applied revision.
type joinResponseEnvelope struct {
	Entries     []joinEntryEnvelope `codec:"en"`
	CorrID      uint64              `codec:"c"`
	StrongIndex uint64              `codec:"si"`
}

// JoinNameEpoch requests the leader's complete registration snapshot for this
// node. The leader first barriers, then captures pending and active names with
// their applied revision while excluding concurrent Apply/Restore operations.
// A commit index alone is insufficient: it can be ahead of applied state.
func (s *Service) JoinNameEpoch(nodeEpoch uint64) (*joinResponseEnvelope, error) {
	if s.raftSvc != nil && s.raftSvc.IsLeader() {
		return s.captureJoinSnapshot(0)
	}
	return s.forwardJoinRequest(nodeEpoch)
}

// captureJoinSnapshot establishes authority before capturing the applied state.
func (s *Service) captureJoinSnapshot(corrID uint64) (*joinResponseEnvelope, error) {
	snapshot, _, err := s.captureEncodedJoinSnapshot(corrID)
	return snapshot, err
}

func (s *Service) captureEncodedJoinSnapshot(corrID uint64) (*joinResponseEnvelope, []byte, error) {
	cfg, release, err := s.acquireJoin()
	if err != nil {
		return nil, nil, err
	}
	defer release()
	if s.raftSvc == nil || s.fsm == nil {
		return nil, nil, global.ErrNotAvailable
	}
	if err := s.raftSvc.Barrier(cfg.Timeout); err != nil {
		return nil, nil, err
	}
	snapshot, err := s.buildJoinSnapshot(corrID, cfg.MaxEntries)
	if err != nil {
		return nil, nil, err
	}
	data, err := encodeJoinSnapshot(snapshot, cfg.MaxBytes)
	if err != nil {
		return nil, nil, err
	}
	return snapshot, data, nil
}

// buildJoinSnapshot captures all registration states and their applied index
// under the same FSM boundary. The caller supplies the authority barrier.
func (s *Service) buildJoinSnapshot(corrID uint64, maxEntries int) (*joinResponseEnvelope, error) {
	resp := &joinResponseEnvelope{CorrID: corrID}
	if s.fsm == nil {
		return nil, global.ErrNotAvailable
	}
	s.fsm.snapshotMu.RLock()
	defer s.fsm.snapshotMu.RUnlock()
	resp.StrongIndex = s.fsm.appliedIndex
	entries, err := s.fsm.state.joinEntries(maxEntries)
	if err != nil {
		return nil, err
	}
	resp.Entries = entries
	return resp, nil
}

// joinEntries copies the wire fields directly from one state view. The caller
// holds the FSM snapshot boundary; shard locks also protect direct state readers.
func (s *shardedState) joinEntries(maxEntries int) ([]joinEntryEnvelope, error) {
	for i := range s.shards {
		s.shards[i].mu.RLock()
	}
	s.pendingMu.RLock()
	defer func() {
		s.pendingMu.RUnlock()
		for i := len(s.shards) - 1; i >= 0; i-- {
			s.shards[i].mu.RUnlock()
		}
	}()
	count := len(s.pending)
	for i := range s.shards {
		count += len(s.shards[i].names)
	}
	if maxEntries <= 0 || count > maxEntries {
		return nil, ErrJoinSnapshotTooLarge
	}
	if count == 0 {
		return nil, nil
	}
	entries := make([]joinEntryEnvelope, 0, count)
	for _, pending := range s.pending {
		entries = append(entries, joinEntryEnvelope{
			Name: pending.Name, Owner: pending.PID, Epoch: pending.Epoch,
			State: joinSnapshotStatePending,
		})
	}
	for i := range s.shards {
		for name, active := range s.shards[i].names {
			state, epoch := joinSnapshotStateConsistent, active.AppliedAt
			if len(active.RequiredNodes) > 0 {
				state, epoch = joinSnapshotStateActive, active.Epoch
			}
			entries = append(entries, joinEntryEnvelope{Name: name, Owner: active.PID, Epoch: epoch, State: state})
		}
	}
	return entries, nil
}

// forwardJoinRequest sends a JoinNameEpoch request through the leader-directed
// write plane and waits for the snapshot reply. Discovers candidates via
// resolveForwardTarget so a non-member (which never observes the leader
// directly) can still pull the snapshot through any raft member.
func (s *Service) forwardJoinRequest(nodeEpoch uint64) (*joinResponseEnvelope, error) {
	cfg, release, err := s.acquireJoin()
	if err != nil {
		return nil, err
	}
	defer release()
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
	perAttempt := cfg.Timeout / time.Duration(attempts)

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
		_, respBody, err := s.captureEncodedJoinSnapshot(env.CorrID)
		if err != nil {
			s.logger.Debug("globalreg: capture join snapshot", zap.Error(err))
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
	env, err := decodeJoinSnapshot(body, s.joinPolicy())
	if err != nil {
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
	case ch <- env:
	default:
	}
}

// runJoinBarrier executes the join-epoch barrier for the given epoch: fetch the
// leader's PENDING∪ACTIVE Strong snapshot, install an exclusion for each entry,
// revoke any conflicting LOCAL/EVENTUAL name this node holds bound to a different
// pid, then flip name_ready — but only if epoch is still the current node epoch
// (a newer rejoin trigger aborts this barrier so the old epoch never flips
// ready). Idempotent: re-running installs the same exclusions (epoch-keyed) and
// re-revokes already-absent names as no-ops.
func (s *Service) runJoinBarrier(epoch uint64) error {
	snap, err := s.JoinNameEpoch(epoch)
	if err != nil {
		return err
	}
	if s.nodeEpoch.Load() != epoch {
		return nil
	}

	for _, e := range snap.Entries {
		if e.State == joinSnapshotStateConsistent {
			continue
		}
		release, err := s.nameGuard.LockContext(context.Background(), e.Name)
		if err != nil {
			return err
		}
		switch e.State {
		case joinSnapshotStateActive:
			s.installSnapshotExclusion(e.Name, e.Owner, e.Epoch, exclusionActive)
		default:
			s.installSnapshotExclusion(e.Name, e.Owner, e.Epoch, exclusionPending)
		}
		s.revokeLocalConflict(e.Name, e.Owner)
		release()
	}

	// Seed the dissem cache with ACTIVE entries (STRONG + CONSISTENT) from the
	// snapshot. PENDING entries are skipped — the cache holds only ACTIVE
	// bindings (a Lookup for a pending name resolves only after promotion).
	s.seedDissemFromSnapshot(snap)

	// Only flip ready if no newer rejoin started while the barrier ran and the
	// leader is still reachable (the snapshot fetch above already proved a leader
	// answered, but a leadership flip mid-barrier is benign — the exclusions are
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

// installSnapshotExclusion latches an exclusion for a snapshot Strong name. It
// installs only when no exclusion at a newer epoch already holds the name, so a
// re-run or a concurrently-latched live pending is never clobbered by a stale
// snapshot. Owner is the reserving pid; a same-name same-epoch exclusion is left
// as-is.
func (s *Service) installSnapshotExclusion(name string, owner pid.PID, epoch uint64, state exclusionState) {
	s.reserveMu.Lock()
	defer s.reserveMu.Unlock()
	if e, ok := s.strongExclusions[name]; ok && e.epoch >= epoch {
		return
	}
	s.strongExclusions[name] = strongExclusion{pid: owner, epoch: epoch, state: state}
}

// revokeLocalConflict drops a LOCAL or EVENTUAL binding this node holds for a
// snapshot Strong name to a pid different from the snapshot owner. The revoker
// signals the losing process. A name not held locally, or held to the snapshot
// owner, is a no-op.
func (s *Service) revokeLocalConflict(name string, owner pid.PID) {
	r := s.loadLocalRevoker()
	if r == nil {
		return
	}
	if r.RevokeLocal(name, owner) {
		s.logger.Info("globalreg: revoked local name lost to strong reservation",
			zap.String("name", name), zap.String("scope", "local"))
	}
	if r.RevokeEventual(name, owner) {
		s.logger.Info("globalreg: revoked local name lost to strong reservation",
			zap.String("name", name), zap.String("scope", "eventual"))
	}
}

// joinBarrierOnStart retries the authoritative snapshot path. That path owns
// the barrier; an ignored extra local barrier cannot establish readiness.
func (s *Service) joinBarrierOnStart() { s.attemptJoinBarrier() }

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
