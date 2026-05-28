// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
)

// alwaysErrTransport returns a fixed error from every Transport method.
// Counts calls so tests can assert how many got through to the inner.
type alwaysErrTransport struct {
	hraft.Transport
	closeCh chan struct{}
	err     error
	calls   int64
}

func newAlwaysErrTransport(err error) *alwaysErrTransport {
	return &alwaysErrTransport{err: err, closeCh: make(chan struct{})}
}

func (a *alwaysErrTransport) Calls() int { return int(atomic.LoadInt64(&a.calls)) }

func (a *alwaysErrTransport) AppendEntries(_ hraft.ServerID, _ hraft.ServerAddress,
	_ *hraft.AppendEntriesRequest, _ *hraft.AppendEntriesResponse) error {
	atomic.AddInt64(&a.calls, 1)
	return a.err
}

func (a *alwaysErrTransport) RequestVote(_ hraft.ServerID, _ hraft.ServerAddress,
	_ *hraft.RequestVoteRequest, _ *hraft.RequestVoteResponse) error {
	atomic.AddInt64(&a.calls, 1)
	return a.err
}

func (a *alwaysErrTransport) RequestPreVote(_ hraft.ServerID, _ hraft.ServerAddress,
	_ *hraft.RequestPreVoteRequest, _ *hraft.RequestPreVoteResponse) error {
	atomic.AddInt64(&a.calls, 1)
	return a.err
}

func (a *alwaysErrTransport) AppendEntriesPipeline(_ hraft.ServerID,
	_ hraft.ServerAddress) (hraft.AppendPipeline, error) {
	atomic.AddInt64(&a.calls, 1)
	return nil, a.err
}

func (a *alwaysErrTransport) Close() error { return nil }

// flipTransport returns errs for the first N calls, then nil.
type flipTransport struct {
	hraft.Transport
	failuresLeft int64
	calls        int64
}

func newFlipTransport(failures int) *flipTransport {
	return &flipTransport{failuresLeft: int64(failures)}
}

func (f *flipTransport) AppendEntries(_ hraft.ServerID, _ hraft.ServerAddress,
	_ *hraft.AppendEntriesRequest, _ *hraft.AppendEntriesResponse) error {
	atomic.AddInt64(&f.calls, 1)
	if atomic.AddInt64(&f.failuresLeft, -1) >= 0 {
		return errors.New("flip: transient")
	}
	return nil
}

func TestPeerStateTracker_ShortsCircuitsAfterFailureLimit(t *testing.T) {
	inner := newAlwaysErrTransport(errors.New("write tcp ...: write: broken pipe"))
	pt := newPeerStateTracker(inner, &telemetry{})
	pt.failureLimit = 3
	pt.backoffInitial = 1 * time.Second
	pt.backoffMax = 1 * time.Second

	target := hraft.ServerAddress("10.0.0.1:7960")
	id := hraft.ServerID("peer-1")
	args := &hraft.AppendEntriesRequest{}
	resp := &hraft.AppendEntriesResponse{}

	// First 3 calls: errors propagate, all reach inner.
	for i := 0; i < 3; i++ {
		err := pt.AppendEntries(id, target, args, resp)
		require.Error(t, err)
	}
	require.Equal(t, 3, inner.Calls(), "first 3 calls must reach the inner transport")

	// 4th and 5th calls: short-circuited with errPeerDead, inner UNTOUCHED.
	for i := 0; i < 2; i++ {
		err := pt.AppendEntries(id, target, args, resp)
		require.ErrorIs(t, err, errPeerDead)
	}
	require.Equal(t, 3, inner.Calls(), "calls in dead window must NOT reach the inner transport")
}

func TestPeerStateTracker_RecoversAfterSuccess(t *testing.T) {
	inner := newFlipTransport(2) // first 2 fail, then succeed
	pt := newPeerStateTracker(inner, &telemetry{})
	pt.failureLimit = 5

	target := hraft.ServerAddress("10.0.0.2:7960")
	id := hraft.ServerID("peer-2")
	args := &hraft.AppendEntriesRequest{}
	resp := &hraft.AppendEntriesResponse{}

	// 2 errors, then 1 success.
	require.Error(t, pt.AppendEntries(id, target, args, resp))
	require.Error(t, pt.AppendEntries(id, target, args, resp))
	require.NoError(t, pt.AppendEntries(id, target, args, resp))

	// Counter must have reset to zero — confirm by sending another call
	// from a fresh failures-left budget on a different fixture.
	pt.mu.Lock()
	consecutive := pt.consecutiveErr[target]
	deadStreak := pt.deadStreak[target]
	pt.mu.Unlock()
	require.Equal(t, 0, consecutive)
	require.Equal(t, 0, deadStreak)
}

func TestPeerStateTracker_BackoffExpiresLetsProbeThrough(t *testing.T) {
	inner := newAlwaysErrTransport(errors.New("write tcp ...: connection reset"))
	pt := newPeerStateTracker(inner, &telemetry{})
	pt.failureLimit = 1
	pt.backoffInitial = 50 * time.Millisecond
	pt.backoffMax = 50 * time.Millisecond

	target := hraft.ServerAddress("10.0.0.3:7960")
	id := hraft.ServerID("peer-3")
	args := &hraft.AppendEntriesRequest{}
	resp := &hraft.AppendEntriesResponse{}

	// One failure marks the peer dead.
	require.Error(t, pt.AppendEntries(id, target, args, resp))
	// Immediate retry: short-circuited.
	require.ErrorIs(t, pt.AppendEntries(id, target, args, resp), errPeerDead)
	require.Equal(t, 1, inner.Calls())

	// Wait past the backoff window.
	time.Sleep(60 * time.Millisecond)
	// Probe goes through (and fails again — but the inner sees it).
	require.Error(t, pt.AppendEntries(id, target, args, resp))
	require.Equal(t, 2, inner.Calls(), "after backoff expiry, one probe must reach the inner")
}

func TestPeerStateTracker_PerPeerIsolation(t *testing.T) {
	// One peer dead must not block traffic to a healthy peer.
	dead := newAlwaysErrTransport(errors.New("dead"))
	pt := newPeerStateTracker(dead, &telemetry{})
	pt.failureLimit = 1
	pt.backoffInitial = time.Hour // never expire

	deadTarget := hraft.ServerAddress("10.0.0.4:7960")
	healthyTarget := hraft.ServerAddress("10.0.0.5:7960")
	args := &hraft.AppendEntriesRequest{}
	resp := &hraft.AppendEntriesResponse{}

	// Mark deadTarget dead.
	require.Error(t, pt.AppendEntries("dead-peer", deadTarget, args, resp))
	require.ErrorIs(t, pt.AppendEntries("dead-peer", deadTarget, args, resp), errPeerDead)

	// Healthy target still reaches the inner. Inner returns errors (it's
	// alwaysErr in this test fixture), but the call is *attempted*.
	pt.AppendEntries("healthy-peer", healthyTarget, args, resp)
	require.Equal(t, 2, dead.Calls(),
		"healthy peer call must reach the inner; only dead peer is short-circuited")
}

// TestPeerStateTracker_SatisfiesWithPreVote pins the contract that the
// tracker exposes the pre-vote RPC. Without this, hashicorp/raft logs
// "pre-vote is disabled because it is not supported by the Transport"
// and partitioned nodes accumulate term increments — which forces
// leader step-down on partition heal (Bug 12).
func TestPeerStateTracker_SatisfiesWithPreVote(t *testing.T) {
	inner := newAlwaysErrTransport(errors.New("transient"))
	pt := newPeerStateTracker(inner, &telemetry{})
	if _, ok := any(pt).(hraft.WithPreVote); !ok {
		t.Fatalf("peerStateTracker must satisfy hraft.WithPreVote so raft can use pre-vote elections")
	}

	args := &hraft.RequestPreVoteRequest{Term: 1}
	resp := &hraft.RequestPreVoteResponse{}
	err := pt.RequestPreVote("peer-1", "10.0.0.1:7960", args, resp)
	require.Error(t, err, "RequestPreVote must surface inner errors")
	require.Equal(t, 1, inner.Calls(),
		"RequestPreVote must forward to the inner transport")
}

// TestInstrumentedTransport_PreservesWithPreVote pins Bug 23: the
// instrumentedTransport wrapper must implement RequestPreVote
// explicitly, because it embeds the hraft.Transport interface (which
// does NOT include RequestPreVote) — the embedded interface does not
// promote methods of its concrete value, so without an explicit
// method here the wrapper fails the WithPreVote assertion at
// peerStateTracker.RequestPreVote and every pre-vote RPC errors,
// causing an election storm under chaos.
func TestInstrumentedTransport_PreservesWithPreVote(t *testing.T) {
	inner := newAlwaysErrTransport(errors.New("transient"))
	wrapped := &instrumentedTransport{Transport: inner, tel: &telemetry{}}
	if _, ok := any(wrapped).(hraft.WithPreVote); !ok {
		t.Fatalf("instrumentedTransport must satisfy hraft.WithPreVote — required for the peerStateTracker chain")
	}

	args := &hraft.RequestPreVoteRequest{Term: 1}
	resp := &hraft.RequestPreVoteResponse{}
	err := wrapped.RequestPreVote("peer-1", "10.0.0.1:7960", args, resp)
	require.Error(t, err, "RequestPreVote must surface inner errors")
	require.Equal(t, 1, inner.Calls(),
		"RequestPreVote must forward to the inner transport")
}

// TestPeerStateTracker_ForgetPeerResetsBackoff verifies that forgetPeer
// (called from Node.OnNodeLeft) clears the accumulated dead-streak and
// dead window for a departing peer. Without this, a pod killed and
// reborn under the same NodeID is trapped behind exponential backoff
// inherited from its previous failing incarnation.
func TestPeerStateTracker_ForgetPeerResetsBackoff(t *testing.T) {
	inner := newAlwaysErrTransport(errors.New("transient"))
	tel := &telemetry{}
	tr := newPeerStateTracker(inner, tel)
	// Tighter knobs so the test doesn't have to wait for backoffInitial.
	tr.failureLimit = 2
	tr.backoffInitial = 50 * time.Millisecond
	tr.backoffMax = 500 * time.Millisecond

	target := hraft.ServerAddress("peer-X")
	args := &hraft.AppendEntriesRequest{Term: 1}
	resp := &hraft.AppendEntriesResponse{}

	// Drive the peer into the dead window: 2 consecutive failures trips,
	// next call short-circuits with errPeerDead.
	for i := 0; i < tr.failureLimit; i++ {
		_ = tr.AppendEntries("peer-X", target, args, resp)
	}
	require.Equal(t, 1, tr.DeadStreak(target), "first dead-window entry")
	err := tr.AppendEntries("peer-X", target, args, resp)
	require.ErrorIs(t, err, errPeerDead,
		"peer must be short-circuited while in dead window")

	// Simulate the peer departing gossip — the tracker must forget it.
	tr.forgetPeer(target)
	require.Equal(t, 0, tr.DeadStreak(target),
		"deadStreak must be cleared by forgetPeer")

	// Next call must reach the inner transport (no short-circuit), even
	// though backoffInitial has not elapsed. A reborn peer accepts the
	// first probe; the inherited dead window would otherwise drop it.
	preCalls := inner.Calls()
	_ = tr.AppendEntries("peer-X", target, args, resp)
	require.Equal(t, preCalls+1, inner.Calls(),
		"forgetPeer must release the short-circuit immediately")
}
