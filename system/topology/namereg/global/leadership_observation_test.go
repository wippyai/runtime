// SPDX-License-Identifier: MPL-2.0

package global

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	"github.com/wippyai/runtime/api/pid"
)

type observedRaft struct {
	*directApplyRaft
	changed chan struct{}
	checked chan bool
	state   raftapi.Leadership
	mu      sync.Mutex
}

func newObservedRaft(fsm *FSM) *observedRaft {
	r := &observedRaft{directApplyRaft: newDirectApplyRaft(fsm, false), changed: make(chan struct{}), checked: make(chan bool, 2)}
	r.state = raftapi.Leadership{State: raftapi.Follower, Term: 1, Changed: r.changed}
	return r
}

func (r *observedRaft) ObserveLeadership() raftapi.Leadership {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *observedRaft) IsLeader() bool {
	current := r.directApplyRaft.IsLeader()
	select {
	case r.checked <- current:
	default:
	}
	return current
}

func (r *observedRaft) publish(state raftapi.State, term uint64, leaderID raftapi.ServerID) {
	r.mu.Lock()
	previous := r.changed
	r.changed = make(chan struct{})
	r.state = raftapi.Leadership{State: state, Term: term, LeaderID: leaderID, Revision: r.state.Revision + 1, Changed: r.changed}
	close(previous)
	r.mu.Unlock()
}

// Raft state, term, and known leader are sampled separately. A mixed sample
// can report Leader/T before the runtime is actually leader. A later change
// of LeaderID in the same state and term must still restore pending timers.
func TestLeadershipCorrectionRestoresPendingTimer(t *testing.T) {
	fsm := NewFSM()
	name := "root.recover"
	epoch := openPending(t, fsm, name, makePID("node-1", "host", "owner"), "node-1",
		[]pid.NodeID{"node-1", "node-2"}, 100)
	// The helper writes a distant deadline; the test only checks that a new
	// leader adopts its pending obligation, not that the timer fires.
	r := newObservedRaft(fsm)
	svc := NewService(r, fsm, &nopBus{}, nil, &nopRouter{}, nil, "node-1", noopLogger(), nil, nil, nil)
	done := make(chan struct{})
	go func() { svc.monitorLeadership(); close(done) }()
	t.Cleanup(func() {
		close(svc.stopCh)
		<-done
	})

	r.publish(raftapi.Leader, 2, "") // mixed sample, not actually leader
	select {
	case leader := <-r.checked:
		require.False(t, leader)
	case <-time.After(time.Second):
		t.Fatal("monitor did not check the mixed leadership sample")
	}
	svc.strongMu.Lock()
	_, premature := svc.strongTimers[strongTimerKey(name, epoch)]
	svc.strongMu.Unlock()
	require.False(t, premature)

	r.leader.Store(true)
	r.publish(raftapi.Leader, 2, "node-1") // correction, same State and Term
	require.Eventually(t, func() bool {
		svc.strongMu.Lock()
		defer svc.strongMu.Unlock()
		_, armed := svc.strongTimers[strongTimerKey(name, epoch)]
		return armed
	}, time.Second, time.Millisecond)
}
