// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
)

// togglingRaft is a follower-role raft stub whose leader reachability the test
// flips at will. reachable=false makes Leader() report no leader so a ping
// fails; reachable=true points it at a leader the cross-router can reach.
type togglingRaft struct {
	fsm       *FSM
	leaderCh  chan bool
	leaderID  raftapi.ServerID
	idx       atomic.Uint64
	reachable atomic.Bool
}

func newTogglingRaft(fsm *FSM, leaderID raftapi.ServerID) *togglingRaft {
	r := &togglingRaft{fsm: fsm, leaderID: leaderID, leaderCh: make(chan bool, 1)}
	r.reachable.Store(true)
	return r
}

func (r *togglingRaft) Apply(data []byte, _ time.Duration) (*raftapi.ApplyResponse, error) {
	return nil, raftapi.ErrNotLeader
}
func (r *togglingRaft) Leader() (raftapi.ServerID, raftapi.ServerAddress, error) {
	if !r.reachable.Load() {
		return "", "", raftapi.ErrNoLeader
	}
	return r.leaderID, r.leaderID + ":0", nil
}
func (r *togglingRaft) IsLeader() bool                { return false }
func (r *togglingRaft) LeaderCh() <-chan bool         { return r.leaderCh }
func (r *togglingRaft) State() raftapi.State          { return raftapi.Follower }
func (r *togglingRaft) Barrier(_ time.Duration) error { return nil }
func (r *togglingRaft) CommitIndex() uint64           { return r.idx.Load() }
func (r *togglingRaft) AddVoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *togglingRaft) AddNonvoter(_ raftapi.ServerID, _ raftapi.ServerAddress, _ time.Duration) error {
	return nil
}
func (r *togglingRaft) DemoteVoter(_ raftapi.ServerID, _ time.Duration) error  { return nil }
func (r *togglingRaft) RemoveServer(_ raftapi.ServerID, _ time.Duration) error { return nil }
func (r *togglingRaft) LeadershipTransfer(_ raftapi.ServerID, _ time.Duration) error {
	return nil
}
func (r *togglingRaft) GetConfiguration() ([]raftapi.Server, error) { return nil, nil }
func (r *togglingRaft) Stats() map[string]string                    { return nil }

// LastContact returns zero so leaderReachable() falls through to the
// wire-level pingLeader path — that's the path these tests exercise.
// The Service.LastContact()-based fast-path is integration-tested in
// the chaos harness with a real raft instance.
func (r *togglingRaft) LastContact() time.Time { return time.Time{} }

// --- No-flap: NodeJoined no longer touches the gate ---

// TestNoFlap_NodeJoinedDoesNotCloseGateOrBumpEpoch proves the old flap is gone.
// The previous design re-triggered the rejoin barrier on every cluster.NodeJoined
// — which fires for ANY peer appearing — so a churny cluster flapped the gate
// closed repeatedly. The fix removes that wiring: the cluster-event handler only
// reacts to NodeLeft, and there is no NodeJoined subscription at all. A burst of
// NodeJoined events through the handler leaves the gate open and the epoch fixed.
func TestNoFlap_NodeJoinedDoesNotCloseGateOrBumpEpoch(t *testing.T) {
	ctx := context.Background()
	svc := newJoinTestService(t)
	require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
	require.True(t, svc.NameReady(), "gate open after first-join barrier")
	epochBefore := svc.nodeEpoch.Load()

	// Drive a burst of NodeJoined events through the surviving cluster-event
	// consumer. The handler has no NodeJoined arm, so each is a no-op: the gate
	// must stay open and the epoch unchanged (the old flap is gone).
	ch := make(chan event.Event, 64)
	for i := 0; i < 50; i++ {
		ch <- event.Event{System: cluster.System, Kind: cluster.NodeJoined,
			Data: cluster.NodeEvent{Node: cluster.NodeInfo{ID: "peer"}}}
	}
	close(ch)
	svc.handleClusterEvents(ctx, ch, event.SubscriberID(""))

	assert.True(t, svc.NameReady(), "NodeJoined must not close the gate (flap fixed)")
	assert.Equal(t, epochBefore, svc.nodeEpoch.Load(), "NodeJoined must not bump the node epoch")
}

// --- Probe: leader reaches itself ---

// TestPingLeader_LeaderSelfReachable proves a leader-role service reports itself
// reachable without any relay traffic.
func TestPingLeader_LeaderSelfReachable(t *testing.T) {
	svc := newJoinTestService(t) // leader-role direct-apply raft
	require.NoError(t, svc.pingLeader(), "leader reaches itself trivially")
}

// TestPingLeader_FollowerRoundTrip proves a follower's ping forwards to the
// leader and resolves on the leader's pong.
func TestPingLeader_FollowerRoundTrip(t *testing.T) {
	xport := &crossRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "node-1", noopLogger(), nil, nil, nil)

	followerFSM := NewFSM()
	followerMem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	followerRaft := newDirectApplyRaft(followerFSM, false)
	followerRaft.knownLeader = "node-1"
	followerSvc := NewService(followerRaft, followerFSM, &nopBus{}, nil, xport, followerMem, "node-2", noopLogger(), nil, nil, nil)

	xport.leader = leaderSvc
	xport.follower = followerSvc

	require.NoError(t, followerSvc.pingLeader(), "follower reaches leader via relay round-trip")
}

// TestPingLeader_NoLeaderFails proves a probe fails when no leader is known.
func TestPingLeader_NoLeaderFails(t *testing.T) {
	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	raft := newTogglingRaft(fsm, "node-1")
	raft.reachable.Store(false)
	svc := NewService(raft, fsm, &nopBus{}, nil, &nopRouter{}, mem, "node-2", noopLogger(), nil, nil, nil)

	assert.Error(t, svc.pingLeader(), "no leader known => probe fails")
}

// --- Reachability close with debounce ---

// TestReachabilityMonitor_ClosesGateAfterGrace proves repeated probe failures
// past the grace flip name_ready false, while a single transient failure within
// the grace does NOT close the gate.
func TestReachabilityMonitor_ClosesGateAfterGrace(t *testing.T) {
	xport := &reachableCrossRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "node-1", noopLogger(), nil, nil, nil)

	fsm := NewFSM()
	mem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	raft := newTogglingRaft(fsm, "node-1")
	svc := NewService(raft, fsm, &nopBus{}, nil, xport, mem, "node-2", noopLogger(), nil, nil, nil)
	svc.probeInterval = 5 * time.Millisecond
	svc.probeGrace = 3
	svc.nameReady.Store(true)
	svc.nodeEpoch.Store(1)

	xport.leader = leaderSvc
	xport.follower = svc

	go svc.monitorLeaderReachability()
	defer close(svc.stopCh)

	// One transient failure (a single tick of unreachability) must NOT close the
	// gate: fewer than grace consecutive failures, then it recovers.
	raft.reachable.Store(false)
	time.Sleep(8 * time.Millisecond) // ~1 failing tick
	raft.reachable.Store(true)
	time.Sleep(30 * time.Millisecond) // several successful ticks reset the counter
	assert.True(t, svc.NameReady(), "single transient failure within grace must not close the gate")

	// Sustained failure past the grace MUST close the gate.
	raft.reachable.Store(false)
	require.Eventually(t, func() bool { return !svc.NameReady() }, time.Second, 5*time.Millisecond,
		"sustained leader loss past grace closes the gate")
}

// --- Reconnect re-barrier ---

// TestReachabilityMonitor_RebarrierOnRecover proves an unreachable->reachable
// transition runs the rejoin barrier: it bumps the node epoch and reopens the
// gate after the snapshot fetch + revoke complete.
func TestReachabilityMonitor_RebarrierOnRecover(t *testing.T) {
	xport := &reachableCrossRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "node-1", noopLogger(), nil, nil, nil)

	followerFSM := NewFSM()
	followerMem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	followerRaft := newTogglingRaft(followerFSM, "node-1")
	followerSvc := NewService(followerRaft, followerFSM, &nopBus{}, nil, xport, followerMem, "node-2", noopLogger(), nil, nil, nil)
	followerSvc.probeInterval = 5 * time.Millisecond
	followerSvc.probeGrace = 2
	followerSvc.nameReady.Store(true)
	followerSvc.nodeEpoch.Store(1)

	xport.leader = leaderSvc
	xport.follower = followerSvc

	go followerSvc.monitorLeaderReachability()
	defer close(followerSvc.stopCh)

	epochBefore := followerSvc.nodeEpoch.Load()

	// Lose the leader: gate closes after grace.
	followerRaft.reachable.Store(false)
	require.Eventually(t, func() bool { return !followerSvc.NameReady() }, time.Second, 5*time.Millisecond,
		"gate closes on sustained leader loss")

	// Regain the leader: the rejoin barrier runs (epoch bump), fetches the
	// snapshot, and reopens the gate.
	followerRaft.reachable.Store(true)
	require.Eventually(t, func() bool { return followerSvc.NameReady() }, 2*time.Second, 5*time.Millisecond,
		"rejoin barrier reopens the gate on recovery")
	assert.Greater(t, followerSvc.nodeEpoch.Load(), epochBefore, "recovery bumps the node epoch (rejoin barrier)")
}

// --- Partition-without-restart conflict coverage ---

// TestReachabilityMonitor_PartitionWithoutRestartRevokesConflict proves the case
// Start-only misses: a node stays UP through a partition, the leader drops it
// from a strong reservation and promotes the name without its ack, and on
// reconnect the recovered-reachability rejoin barrier installs the exclusion and
// revokes the now-conflicting LOCAL name the node still holds.
func TestReachabilityMonitor_PartitionWithoutRestartRevokesConflict(t *testing.T) {
	xport := &reachableCrossRouter{}

	leaderFSM := NewFSM()
	leaderMem := &fakeMembership{local: "node-1", ids: []string{"node-1", "node-2"}}
	leaderSvc := NewService(newDirectApplyRaft(leaderFSM, true), leaderFSM, &nopBus{}, nil, xport, leaderMem, "node-1", noopLogger(), nil, nil, nil)

	followerFSM := NewFSM()
	followerMem := &fakeMembership{local: "node-2", ids: []string{"node-1", "node-2"}}
	followerRaft := newTogglingRaft(followerFSM, "node-1")
	followerSvc := NewService(followerRaft, followerFSM, &nopBus{}, nil, xport, followerMem, "node-2", noopLogger(), nil, nil, nil)
	followerSvc.probeInterval = 5 * time.Millisecond
	followerSvc.probeGrace = 2
	followerSvc.nameReady.Store(true)
	followerSvc.nodeEpoch.Store(1)

	// The follower stays UP and still holds the name bound LOCAL to its own pid.
	rev := newRecordingRevoker()
	rev.local["system.partition"] = makePID("node-2", "host", "stale-local")
	followerSvc.SetLocalNameRevoker(rev)

	xport.leader = leaderSvc
	xport.follower = followerSvc

	go followerSvc.monitorLeaderReachability()
	defer close(followerSvc.stopCh)

	// While the follower is partitioned, the leader admits + promotes the strong
	// name to a DIFFERENT owner without the follower's ack (the CmdDropRequired
	// drop-and-promote path).
	followerRaft.reachable.Store(false)
	require.Eventually(t, func() bool { return !followerSvc.NameReady() }, time.Second, 5*time.Millisecond,
		"gate closes during the partition")
	strongOwner := makePID("node-3", "host", "strong-owner")
	seedActiveStrong(t, leaderFSM, "system.partition", strongOwner, []pid.NodeID{"node-1"}, 1500)

	// Reconnect: the rejoin barrier must install the exclusion AND revoke the
	// conflicting local name before reopening the gate.
	followerRaft.reachable.Store(true)
	require.Eventually(t, func() bool { return followerSvc.NameReady() }, 2*time.Second, 5*time.Millisecond,
		"rejoin barrier reopens the gate after reconnect")

	reserved, ok := followerSvc.IsStrongReserved("system.partition")
	require.True(t, ok, "reconnect barrier installs the strong exclusion missed during the partition")
	assert.Equal(t, strongOwner, reserved, "exclusion surfaces the strong owner as taken")
	assert.Contains(t, rev.revokedLoc, "system.partition", "conflicting local name revoked on reconnect")
}

// reachableCrossRouter routes ping AND join traffic both ways between a leader
// and a follower service. Unlike crossRouter it dispatches every topic, so the
// reachability monitor's ping and the barrier's join both resolve.
type reachableCrossRouter struct {
	leader   *Service
	follower *Service
}

func (r *reachableCrossRouter) Send(pkg *relay.Package) error {
	target := pkg.Target.Node
	src := pkg.Source.Node
	for _, m := range pkg.Messages {
		var body []byte
		if len(m.Payloads) > 0 {
			if b, ok := m.Payloads[0].Data().([]byte); ok {
				body = append(body, b...)
			}
		}
		dst := r.leader
		if target == "node-2" {
			dst = r.follower
		}
		fresh := relay.NewServicePackage(src, HostID, target, HostID, m.Topic, payload.New(body))
		_ = dst.Send(fresh)
	}
	relay.ReleasePackage(pkg)
	return nil
}
