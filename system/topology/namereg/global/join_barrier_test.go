// SPDX-License-Identifier: MPL-2.0

package global

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// newJoinTestService wires a leader-role service with a direct-apply raft and a
// fake membership. nodeEpoch is seeded to 1 (as Start would) so barrier runs
// behave like a real first-join.
func newJoinTestService(t *testing.T) *Service {
	t.Helper()
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-1", ids: []string{"node-1"}}
	svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-1", noopLogger(), nil, nil, nil)
	svc.mu.Lock()
	svc.ready = true
	svc.mu.Unlock()
	svc.nodeEpoch.Store(1)
	return svc
}

// seedActiveStrong promotes a Strong name to active on the FSM so it appears in
// the join snapshot as an ACTIVE entry.
func seedActiveStrong(t *testing.T, fsm *FSM, name string, owner pid.PID, required []pid.NodeID, openIdx uint64) {
	t.Helper()
	promoteToActive(t, fsm, name, owner, required, openIdx)
}

// --- Snapshot enumeration: ACTIVE strong vs Consistent ---

// TestJoinSnapshot_ActiveStrongDistinctFromConsistent proves the snapshot lists
// a promoted Strong name (carries RequiredNodes) AND a plain Consistent
// register, distinguished by the State byte. Consistent entries seed the
// dissem cache without installing a strong observation.
func TestJoinSnapshot_ActiveStrongDistinctFromConsistent(t *testing.T) {
	svc := newJoinTestService(t)
	fsm := svc.fsm

	owner := makePID("node-2", "host", "strong")
	seedActiveStrong(t, fsm, "system.strong", owner, []pid.NodeID{"node-1"}, 100)

	// A Consistent register appears in the snapshot tagged as Consistent so the
	// joining node seeds its dissem cache for it.
	cons := makePID("node-3", "host", "cons")
	applyAt(t, fsm, &Command{Type: CmdRegister, Name: "system.consistent", PID: cons, NodeID: "node-3"}, 200)

	snap, err := svc.JoinNameEpoch(svc.nodeEpoch.Load())
	require.NoError(t, err)

	names := map[string]uint8{}
	for _, e := range snap.Entries {
		names[e.Name] = e.State
	}
	require.Contains(t, names, "system.strong", "active strong name in snapshot")
	assert.Equal(t, joinSnapshotStateActive, names["system.strong"])
	require.Contains(t, names, "system.consistent", "consistent entry tagged in snapshot")
	assert.Equal(t, joinSnapshotStateConsistent, names["system.consistent"])
}

// TestJoinSnapshot_IncludesPending proves an in-flight PENDING strong reservation
// appears in the snapshot marked Pending. This is the restart-hole coverage.
func TestJoinSnapshot_IncludesPending(t *testing.T) {
	svc := newJoinTestService(t)
	fsm := svc.fsm
	// Detach the self-ack hook so the pending stays pending.
	fsm.SetOnPending(nil)

	owner := makePID("node-2", "host", "pend")
	openPending(t, fsm, "system.pending", owner, "node-2", []pid.NodeID{"node-1", "node-2"}, 300)

	snap, err := svc.JoinNameEpoch(svc.nodeEpoch.Load())
	require.NoError(t, err)

	var found bool
	for _, e := range snap.Entries {
		if e.Name == "system.pending" {
			found = true
			assert.Equal(t, joinSnapshotStatePending, e.State)
			assert.Equal(t, owner, e.Owner)
		}
	}
	assert.True(t, found, "pending strong reservation in snapshot")
}

// TestJoinSnapshot_CarriesCommitIndex proves the snapshot stamps strong_index from
// the raft commit index.
func TestJoinSnapshot_CarriesCommitIndex(t *testing.T) {
	svc := newJoinTestService(t)
	// Drive a register through the service so the raft stub advances its applied
	// (commit) index; the snapshot must source strong_index from it.
	_, err := svc.applyCommand(&Command{Type: CmdRegister, Name: "system.idx", PID: makePID("node-2", "host", "p"), NodeID: "node-2"})
	require.NoError(t, err)

	snap, err := svc.JoinNameEpoch(svc.nodeEpoch.Load())
	require.NoError(t, err)
	assert.Equal(t, svc.raftSvc.CommitIndex(), snap.StrongIndex, "strong_index from commit index")
	assert.NotZero(t, snap.StrongIndex)
}

// --- The joined-after-ACTIVE gap proof ---

// --- Gate ---

// TestNameReady_FalseUntilBarrier proves NameReady starts false and flips only
// after a successful barrier.
func TestNameReady_FalseUntilBarrier(t *testing.T) {
	svc := newJoinTestService(t)
	assert.False(t, svc.NameReady(), "gate closed before the barrier")
	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
	assert.True(t, svc.NameReady(), "gate open after the barrier")
}

// --- Epoch abort ---

// TestBarrier_EpochBumpAbortsStaleReady proves an epoch bump during a barrier
// aborts the stale barrier: it does not flip ready for the old epoch.
func TestBarrier_EpochBumpAbortsStaleReady(t *testing.T) {
	svc := newJoinTestService(t)
	staleEpoch := svc.nodeEpoch.Load()

	// Simulate a rejoin trigger landing mid-barrier: bump the epoch, then run the
	// barrier for the now-stale epoch. It must not flip ready.
	svc.nodeEpoch.Add(1)
	require.NoError(t, svc.runJoinBarrier(staleEpoch))
	assert.False(t, svc.NameReady(), "stale-epoch barrier must not open the gate")

	// The current-epoch barrier does open it.
	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
	assert.True(t, svc.NameReady())
}

// --- Idempotency ---

// TestBarrier_RerunIdempotent proves running the barrier twice converges with no
// leaked or clobbered Strong observations.
func TestBarrier_RerunIdempotent(t *testing.T) {
	svc := newJoinTestService(t)
	owner := makePID("node-2", "host", "o")
	seedActiveStrong(t, svc.fsm, "system.idem", owner, []pid.NodeID{"node-1"}, 900)

	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))

	reserved, ok := svc.IsStrongReserved("system.idem")
	require.True(t, ok)
	assert.Equal(t, owner, reserved, "observation intact after rerun")
}

// TestInstallSnapshotObservation_DoesNotClobberNewer proves a stale snapshot install
// never overwrites a higher-epoch observation already held (a live pending latched
// concurrently).
func TestInstallSnapshotObservation_DoesNotClobberNewer(t *testing.T) {
	svc := newJoinTestService(t)
	newer := makePID("node-9", "host", "newer")
	svc.strongObservations["system.race"] = strongObservation{pid: newer, epoch: 50, state: observationActive}

	// A stale snapshot at epoch 10 must not clobber the epoch-50 observation.
	svc.installSnapshotObservation("system.race", makePID("node-2", "host", "old"), 10, observationPending)
	reserved, ok := svc.IsStrongReserved("system.race")
	require.True(t, ok)
	assert.Equal(t, newer, reserved, "newer observation preserved against stale snapshot")
}

// --- Non-leader forwarding ---

// TestJoinNameEpoch_ForwardsToLeader proves a non-leader forwards JoinNameEpoch to
// the leader over the relay and resolves the snapshot from the leader's reply.
func TestJoinNameEpoch_ForwardsToLeader(t *testing.T) {
	// A router that routes the follower's join request into the leader's Send
	// and the leader's reply back to the follower. Both services share it so the
	// reply path is not dropped.
	xport := &crossRouter{}

	// Leader service holds the authoritative state.
	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	leaderRaft := newDirectApplyRaft(leaderFSM, true)
	leaderSvc := NewService(leaderRaft, leaderFSM, &nopBus{}, nil, xport, leaderMem, "node-1", noopLogger(), nil, nil, nil)
	seedActiveStrong(t, leaderFSM, "system.fwd", makePID("node-1", "host", "o"), []pid.NodeID{"node-1"}, 1000)

	// Follower with an empty FSM (non-member shape).
	followerFSM := NewFSM()
	followerMem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	followerRaft := newDirectApplyRaft(followerFSM, false)
	followerRaft.knownLeader = "node-1"
	followerSvc := NewService(followerRaft, followerFSM, &nopBus{}, nil, xport, followerMem, "node-2", noopLogger(), nil, nil, nil)
	xport.leader = leaderSvc
	xport.follower = followerSvc

	snap, err := followerSvc.JoinNameEpoch(2)
	require.NoError(t, err)
	var found bool
	for _, e := range snap.Entries {
		if e.Name == "system.fwd" {
			found = true
		}
	}
	assert.True(t, found, "follower learns the active strong name via leader forward")
}

// crossRouter wires a follower's join request to a leader's Send handler and
// routes the leader's join response back. It only needs to handle the join
// topics for the forward test.
type crossRouter struct {
	leader   *Service
	follower *Service
}

func (r *crossRouter) Send(pkg *relay.Package) error {
	target := pkg.Target.Node
	for _, m := range pkg.Messages {
		var body []byte
		if len(m.Payloads) > 0 {
			if b, ok := m.Payloads[0].Data().([]byte); ok {
				body = append(body, b...)
			}
		}
		// Re-wrap into a fresh package the destination service can dispatch.
		dst := r.leader
		if target == "node-2" {
			dst = r.follower
		}
		fresh := relay.NewServicePackage(pkg.Source.Node, HostID, target, HostID, m.Topic, payload.New(body))
		_ = dst.Send(fresh)
	}
	relay.ReleasePackage(pkg)
	return nil
}

// --- Epoch-scoped nudge drop ---

// TestCheckPending_DropsStaleEpochNudge proves a nudge carrying a node epoch that
// does not match the recipient's current epoch is dropped (no re-ack), closing
// the restart hole at the nudge path.
func TestCheckPending_DropsStaleEpochNudge(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	router := &capturingRouter{}
	raftStub := newDirectApplyRaft(fsm, false)
	raftStub.knownLeader = "node-1"
	svc := NewService(raftStub, fsm, &nopBus{}, nil, router, mem, "node-2", noopLogger(), nil, nil, nil)
	svc.nodeEpoch.Store(5)
	fsm.SetOnPending(nil)

	epoch := openPending(t, fsm, "system.stalenudge", makePID("node-1", "host", "p"), "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 1100)

	// Nudge addressed to a PRIOR incarnation (node epoch 3 != current 5).
	body, err := marshalMsgpack(checkPendingEnvelope{Name: "system.stalenudge", Epoch: epoch, NodeEpoch: 3})
	require.NoError(t, err)
	pkg := relay.NewServicePackage("node-1", HostID, "node-2", HostID, topicCheckPending, payload.New(body))
	require.NoError(t, svc.Send(pkg))

	assert.Empty(t, router.byTopic(topicRegisterAck), "stale-epoch nudge dropped, no re-ack")

	// A nudge at the current epoch (5) IS acted upon.
	body2, err := marshalMsgpack(checkPendingEnvelope{Name: "system.stalenudge", Epoch: epoch, NodeEpoch: 5})
	require.NoError(t, err)
	pkg2 := relay.NewServicePackage("node-1", HostID, "node-2", HostID, topicCheckPending, payload.New(body2))
	require.NoError(t, svc.Send(pkg2))
	assert.NotEmpty(t, router.byTopic(topicRegisterAck), "current-epoch nudge produces a re-ack")
}

// TestAck_CarriesNodeEpoch proves a follower's ack stamps its current node epoch
// so the leader can record it for later nudge addressing.
func TestAck_CarriesNodeEpoch(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	router := &capturingRouter{}
	raftStub := newDirectApplyRaft(fsm, false)
	raftStub.knownLeader = "node-1"
	svc := NewService(raftStub, fsm, &nopBus{}, nil, router, mem, "node-2", noopLogger(), nil, nil, nil)
	svc.nodeEpoch.Store(7)

	require.NoError(t, svc.sendAck("system.ackep", 42))
	acks := router.byTopic(topicRegisterAck)
	require.Len(t, acks, 1)
	var env ackEnvelope
	require.NoError(t, unmarshalMsgpack(acks[0].body, &env))
	assert.Equal(t, uint64(7), env.NodeEpoch, "ack carries the acker's node epoch")
}
