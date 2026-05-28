// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"errors"
	"sync"
	"time"

	hraft "github.com/hashicorp/raft"
)

// errPeerDead is returned by peerStateTracker calls that short-circuit
// because the target peer is in the dead-backoff window. Callers
// (the raft engine) treat it like any transport error and retry per
// their own policy — but they retry against US, where we cheaply
// return errPeerDead again until the backoff expires. Net effect: no
// TCP write to a known-dead socket while the backoff is in effect.
var errPeerDead = errors.New("raft-net: peer dead, backoff in effect")

// peerStateTracker wraps an hraft.Transport so consecutive write
// failures per peer are tracked and the transport short-circuits
// further writes to a known-dead peer until an exponential backoff
// expires.
//
// Without this, hashicorp/raft's TCPTransport blindly retries writes
// on dead sockets — under chaos network-partition we observed thousands
// of `write tcp ...: write: broken pipe` lines per second per peer.
// Rate-limiting the *log* (raftStderrAdapter) made the logs quiet but
// did not stop the underlying retry-on-dead-socket behavior.
//
// The tracker is conservative:
//   - First success against a peer clears the failure counter and
//     ends the backoff window immediately.
//   - Failures only count as "consecutive" until `failureLimit`; once
//     the peer is marked dead, the backoff doubles per consecutive
//     dead-window expiry up to `backoffMax`.
//   - The backoff is per-peer; one dead peer never blocks calls to
//     others.
type peerStateTracker struct {
	hraft.Transport
	tel *telemetry

	consecutiveErr map[hraft.ServerAddress]int
	deadUntil      map[hraft.ServerAddress]time.Time
	deadStreak     map[hraft.ServerAddress]int

	backoffInitial time.Duration
	backoffMax     time.Duration

	mu           sync.Mutex
	failureLimit int
}

const (
	defaultFailureLimit   = 5
	defaultBackoffInitial = 100 * time.Millisecond
	defaultBackoffMax     = 5 * time.Second
)

func newPeerStateTracker(inner hraft.Transport, tel *telemetry) *peerStateTracker {
	return &peerStateTracker{
		Transport:      inner,
		tel:            tel,
		consecutiveErr: make(map[hraft.ServerAddress]int),
		deadUntil:      make(map[hraft.ServerAddress]time.Time),
		deadStreak:     make(map[hraft.ServerAddress]int),
		failureLimit:   defaultFailureLimit,
		backoffInitial: defaultBackoffInitial,
		backoffMax:     defaultBackoffMax,
	}
}

// Close is forwarded so the wrapper still satisfies hraft.WithClose
// when the inner transport supports it.
func (t *peerStateTracker) Close() error {
	if closer, ok := t.Transport.(hraft.WithClose); ok {
		return closer.Close()
	}
	return nil
}

// AppendEntries is the hottest path under partition. Short-circuit
// when the peer is in the dead-backoff window.
func (t *peerStateTracker) AppendEntries(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.AppendEntriesRequest, resp *hraft.AppendEntriesResponse) error {
	if t.isDead(target) {
		t.tel.recordPeerDeadSkip(string(id))
		return errPeerDead
	}
	err := t.Transport.AppendEntries(id, target, args, resp)
	t.recordResult(target, string(id), err)
	return err
}

func (t *peerStateTracker) RequestVote(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.RequestVoteRequest, resp *hraft.RequestVoteResponse) error {
	if t.isDead(target) {
		t.tel.recordPeerDeadSkip(string(id))
		return errPeerDead
	}
	err := t.Transport.RequestVote(id, target, args, resp)
	t.recordResult(target, string(id), err)
	return err
}

// RequestPreVote satisfies hraft.WithPreVote so the raft engine can send
// pre-vote RPCs through this transport. Without this method, hashicorp/raft
// logs "pre-vote is disabled because it is not supported by the Transport"
// at startup and falls back to immediate term-bumping elections — a
// partitioned node accumulates term increments during isolation and forces
// a leader step-down when it rejoins, even though the cluster was healthy.
//
// Implementation mirrors RequestVote: short-circuit if the peer is in the
// dead-backoff window, otherwise forward to the inner transport and feed
// the result into the per-peer failure tracker.
//
// The inner is hraft.NetworkTransport (TCPTransport in v1.7.3+) which
// implements RequestPreVote natively. If a future caller wraps a transport
// that doesn't, the type assertion below panics with a clear message
// rather than silently semantically downgrading to a regular RequestVote
// (which would cause the very term inflation pre-vote is meant to prevent).
func (t *peerStateTracker) RequestPreVote(id hraft.ServerID, target hraft.ServerAddress,
	args *hraft.RequestPreVoteRequest, resp *hraft.RequestPreVoteResponse) error {
	if t.isDead(target) {
		t.tel.recordPeerDeadSkip(string(id))
		return errPeerDead
	}
	withPV, ok := t.Transport.(hraft.WithPreVote)
	if !ok {
		return errors.New("raft-net: inner transport does not implement WithPreVote; remove peerStateTracker.RequestPreVote or wrap a pre-vote-capable transport")
	}
	err := withPV.RequestPreVote(id, target, args, resp)
	t.recordResult(target, string(id), err)
	return err
}

// AppendEntriesPipeline wraps the underlying pipeline so failures on
// pipelined writes also count toward the consecutive-error tally.
func (t *peerStateTracker) AppendEntriesPipeline(id hraft.ServerID,
	target hraft.ServerAddress) (hraft.AppendPipeline, error) {
	if t.isDead(target) {
		t.tel.recordPeerDeadSkip(string(id))
		return nil, errPeerDead
	}
	inner, err := t.Transport.AppendEntriesPipeline(id, target)
	if err != nil {
		t.recordResult(target, string(id), err)
		return nil, err
	}
	// On open success, reset the counter — the pipeline is up.
	t.recordResult(target, string(id), nil)
	return inner, nil
}

// isDead returns true if the peer is currently in the dead window.
// Side-effect free except for clearing the entry once the window
// expires (which is also done with the mu held).
func (t *peerStateTracker) isDead(target hraft.ServerAddress) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	until, ok := t.deadUntil[target]
	if !ok {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	// Window expired — let one probe through. The probe outcome will
	// either fully reset the streak (success) or extend the streak
	// (further failure).
	delete(t.deadUntil, target)
	return false
}

// DeadStreak returns how many times this peer has tripped the failure
// threshold consecutively. Used by the MembershipHandler to decide whether
// to proactively evict a voter that gossip ALSO sees as suspect/dead, rather
// than waiting for the gossip expiration.
func (t *peerStateTracker) DeadStreak(target hraft.ServerAddress) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.deadStreak[target]
}

// forgetPeer drops all accumulated failure state for target. Called on
// cluster.NodeLeft so a rebirth of a pod (same NodeID, fresh process,
// fresh yamux session) does not inherit the exponential backoff that
// accumulated while the old incarnation was failing. Without this, a
// killed pod's NodeID could sit in the dead window for up to backoffMax
// after a fresh process is already accepting connections, leaving the
// reborn peer effectively unreachable until the timer expires.
func (t *peerStateTracker) forgetPeer(target hraft.ServerAddress) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.consecutiveErr, target)
	delete(t.deadStreak, target)
	delete(t.deadUntil, target)
}

// recordResult updates the per-peer counters after a transport call.
// On success: full reset. On failure: bump consecutive counter, and
// once the threshold is hit, mark the peer dead with exponential
// backoff scaled by the dead-streak length.
func (t *peerStateTracker) recordResult(target hraft.ServerAddress, id string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err == nil {
		if t.consecutiveErr[target] > 0 || t.deadStreak[target] > 0 {
			t.tel.recordPeerRecovered(id)
		}
		delete(t.consecutiveErr, target)
		delete(t.deadStreak, target)
		delete(t.deadUntil, target)
		return
	}

	// Don't double-count our own short-circuit.
	if errors.Is(err, errPeerDead) {
		return
	}

	t.consecutiveErr[target]++
	if t.consecutiveErr[target] < t.failureLimit {
		return
	}

	// Threshold tripped: schedule the dead window. Streak grows so
	// chronic offenders back off longer (capped at backoffMax).
	t.deadStreak[target]++
	backoff := t.backoffInitial << uint(t.deadStreak[target]-1)
	if backoff > t.backoffMax || backoff <= 0 {
		backoff = t.backoffMax
	}
	t.deadUntil[target] = time.Now().Add(backoff)
	t.tel.recordPeerDead(id, backoff)

	// Reset the counter so the next failure-after-recovery starts
	// fresh rather than instantly re-tripping.
	t.consecutiveErr[target] = 0
}
