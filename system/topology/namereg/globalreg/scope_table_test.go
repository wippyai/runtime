// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/topology/namereg/globalreg"
)

// newTestService constructs a Service backed by a direct-apply Raft and
// flagged as ready so Lookup returns the FSM-resident entry.
func newTestService(fsm *FSM, leader bool) *Service {
	svc := NewService(newDirectApplyRaft(fsm, leader), fsm, &nopBus{}, nil, &nopRouter{}, nil, "node-1", noopLogger(), nil, nil, nil)
	svc.mu.Lock()
	svc.ready = true
	svc.mu.Unlock()
	return svc
}

// TestScopeTable_Consistent verifies the Consistent path through
// RegisterScope and UnregisterScope using a stub Raft service that
// directly applies the encoded command to the FSM.
func TestScopeTable_Consistent(t *testing.T) {
	fsm := NewFSM()
	svc := newTestService(fsm, true)

	p := makePID("node-1", "host", "1")
	out, err := svc.RegisterScope(context.Background(), "svc", p, globalreg.Consistent)
	require.NoError(t, err)
	assert.Equal(t, p, out.PID)
	assert.Equal(t, globalreg.RegisterStateActive, out.State)
	assert.NotZero(t, out.Epoch)

	res, err := svc.Lookup(context.Background(), "svc")
	require.NoError(t, err)
	assert.True(t, res.Found)
	assert.Equal(t, p, res.PID)
	gotPID, gotIdx, found := fsm.State().LookupWithIndex("svc")
	require.True(t, found)
	assert.Equal(t, p, gotPID)
	assert.Equal(t, out.Epoch, gotIdx)

	removed, err := svc.UnregisterScope(context.Background(), "svc", globalreg.Consistent)
	require.NoError(t, err)
	assert.True(t, removed)
}

// TestConsistentScope_ConcurrentCrossNode reproduces the reviewer's
// requested scenario from PR #241 issue 4494041044 test item 1:
// "concurrent global registration of the same name from multiple
// nodes". Two services representing node-A and node-B share one Raft
// log (the directApplyRaft serializes Apply through a single atomic
// counter, mirroring real Raft's single-leader commit pipeline) and
// concurrently call RegisterScope on the same name with their own
// PIDs. The invariant: exactly one becomes Active, exactly one gets
// ErrNameAlreadyRegistered carrying the winner's PID as ExistingPID,
// and both sides see the same authoritative PID via Lookup.
func TestConsistentScope_ConcurrentCrossNode(t *testing.T) {
	fsm := NewFSM()
	raft := newDirectApplyRaft(fsm, true)

	newSvc := func(nodeID string) *Service {
		s := NewService(raft, fsm, &nopBus{}, nil, &nopRouter{}, nil, nodeID, noopLogger(), nil, nil, nil)
		s.mu.Lock()
		s.ready = true
		s.mu.Unlock()
		return s
	}
	svcA := newSvc("node-A")
	svcB := newSvc("node-B")

	pA := makePID("node-A", "host", "1")
	pB := makePID("node-B", "host", "1")

	const name = "svc.consistent.cross"

	type result struct {
		err error
		who string
		out globalreg.RegisterOutcome
	}
	results := make(chan result, 2)

	var start sync.WaitGroup
	start.Add(1)

	go func() {
		start.Wait()
		out, err := svcA.RegisterScope(context.Background(), name, pA, globalreg.Consistent)
		results <- result{who: "A", out: out, err: err}
	}()

	go func() {
		start.Wait()
		out, err := svcB.RegisterScope(context.Background(), name, pB, globalreg.Consistent)
		results <- result{who: "B", out: out, err: err}
	}()

	start.Done()

	r1 := <-results
	r2 := <-results

	wins := 0
	conflicts := 0
	var winner result
	var loser result
	for _, r := range []result{r1, r2} {
		switch {
		case r.err == nil && r.out.State == globalreg.RegisterStateActive:
			wins++
			winner = r
		case errors.Is(r.err, globalreg.ErrNameAlreadyRegistered):
			conflicts++
			loser = r
		default:
			t.Fatalf("unexpected outcome from %s: out=%+v err=%v", r.who, r.out, r.err)
		}
	}

	require.Equal(t, 1, wins, "exactly one register call must succeed")
	require.Equal(t, 1, conflicts, "exactly one register call must hit ErrNameAlreadyRegistered")

	expectedWinnerPID := pA
	if winner.who == "B" {
		expectedWinnerPID = pB
	}
	assert.Equal(t, expectedWinnerPID, winner.out.PID, "winner's outcome carries its own PID")
	assert.NotZero(t, winner.out.Epoch, "winner has a registration epoch")
	assert.Equal(t, expectedWinnerPID, loser.out.ExistingPID, "loser's outcome carries the winner's PID")

	for _, svc := range []*Service{svcA, svcB} {
		res, err := svc.Lookup(context.Background(), name)
		require.NoError(t, err)
		require.True(t, res.Found, "both services must see the registration")
		assert.Equal(t, expectedWinnerPID, res.PID, "lookup PID must equal the winner across both services")
		_, gotIdx, found := svc.fsm.State().LookupWithIndex(name)
		require.True(t, found)
		assert.Equal(t, winner.out.Epoch, gotIdx, "registration epoch must equal the winner's epoch")
	}
}

// TestScopeTable_RegisterDefaultsToConsistent verifies that Register (the
// no-mode convenience over RegisterScope) registers and unregisters a name.
func TestScopeTable_RegisterDefaultsToConsistent(t *testing.T) {
	fsm := NewFSM()
	svc := newTestService(fsm, true)

	p := makePID("node-1", "host", "1")
	got, err := svc.Register(context.Background(), "svc", p)
	require.NoError(t, err)
	assert.Equal(t, p, got)

	ok, err := svc.Unregister(context.Background(), "svc")
	require.NoError(t, err)
	assert.True(t, ok)
}
