// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"errors"
	"sync"
	"sync/atomic"
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
//
// The hot path (AppendEntries / RequestVote / RequestPreVote /
// AppendEntriesPipeline) is fully lock-free: per-peer state lives in a
// sync.Map (lock-free Load for known keys; brief contention only on the
// very first call per peer) and each peer's state is held in atomic
// fields. Threshold trips are claimed via CAS so concurrent failing
// senders against the same peer cannot double-count a single trip.
type peerStateTracker struct {
	hraft.Transport
	tel *telemetry

	// peers is a map[hraft.ServerAddress]*peerState. Lock-free reads on
	// the hot path; LoadOrStore on first-write per peer.
	peers sync.Map

	backoffInitial time.Duration
	backoffMax     time.Duration

	failureLimit int
}

// peerState holds per-peer failure-tracker state. All fields are atomic
// so the AppendEntries / RequestVote hot path needs no mutex.
//
// deadUntilUnixNano == 0 means "not in a dead window". A non-zero value
// is the deadline (UnixNano); reads compare against time.Now().UnixNano().
type peerState struct {
	consecutiveErr    atomic.Int32
	deadStreak        atomic.Int32
	deadUntilUnixNano atomic.Int64
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
// Lock-free: one atomic.Load on deadUntilUnixNano. If the window has
// expired, the function clears the deadline (best-effort CAS) so the
// next caller's recordResult writes against a clean slate.
func (t *peerStateTracker) isDead(target hraft.ServerAddress) bool {
	ps := t.lookup(target)
	if ps == nil {
		return false
	}
	until := ps.deadUntilUnixNano.Load()
	if until == 0 {
		return false
	}
	if time.Now().UnixNano() < until {
		return true
	}
	// Window expired — try to clear the deadline. If another caller wins
	// the CAS, fine; either way the entry is no longer "dead".
	ps.deadUntilUnixNano.CompareAndSwap(until, 0)
	return false
}

// DeadStreak returns how many times this peer has tripped the failure
// threshold consecutively. Used by the MembershipHandler to decide whether
// to proactively evict a voter that gossip ALSO sees as suspect/dead, rather
// than waiting for the gossip expiration.
func (t *peerStateTracker) DeadStreak(target hraft.ServerAddress) int {
	ps := t.lookup(target)
	if ps == nil {
		return 0
	}
	return int(ps.deadStreak.Load())
}

// forgetPeer drops all accumulated failure state for target. Called on
// cluster.NodeLeft so a rebirth of a pod (same NodeID, fresh process,
// fresh yamux session) does not inherit the exponential backoff that
// accumulated while the old incarnation was failing. Without this, a
// killed pod's NodeID could sit in the dead window for up to backoffMax
// after a fresh process is already accepting connections, leaving the
// reborn peer effectively unreachable until the timer expires.
func (t *peerStateTracker) forgetPeer(target hraft.ServerAddress) {
	t.peers.Delete(target)
}

// recordResult updates the per-peer counters after a transport call.
// On success: full reset of all three atomic counters. On failure:
// atomic add to consecutiveErr; when the threshold is hit, claim the
// trip via CAS so only one concurrent failure registers the dead window.
func (t *peerStateTracker) recordResult(target hraft.ServerAddress, id string, err error) {
	if err == nil {
		ps := t.lookup(target)
		if ps == nil {
			return
		}
		// recovered? Fire telemetry if any tracked field was non-zero.
		// The three Loads are independent atomics; "any non-zero" is a
		// best-effort signal and doesn't need to be a snapshot.
		if ps.consecutiveErr.Load() > 0 || ps.deadStreak.Load() > 0 || ps.deadUntilUnixNano.Load() > 0 {
			t.tel.recordPeerRecovered(id)
		}
		ps.consecutiveErr.Store(0)
		ps.deadStreak.Store(0)
		ps.deadUntilUnixNano.Store(0)
		return
	}

	// Don't double-count our own short-circuit.
	if errors.Is(err, errPeerDead) {
		return
	}

	ps := t.getOrCreate(target)
	n := ps.consecutiveErr.Add(1)
	if int(n) < t.failureLimit {
		return
	}

	// Threshold tripped — try to CLAIM the trip via CAS. Only the
	// goroutine that successfully resets consecutiveErr from n→0
	// proceeds to schedule the dead window; concurrent failing senders
	// who saw n>=failureLimit but lose the CAS skip the trip (the
	// winner already scheduled it). This prevents two threads both
	// observing n>=failureLimit from each incrementing deadStreak.
	if !ps.consecutiveErr.CompareAndSwap(n, 0) {
		return
	}

	streak := ps.deadStreak.Add(1)
	backoff := t.backoffInitial << uint(streak-1)
	if backoff > t.backoffMax || backoff <= 0 {
		backoff = t.backoffMax
	}
	ps.deadUntilUnixNano.Store(time.Now().Add(backoff).UnixNano())
	t.tel.recordPeerDead(id, backoff)
}

// lookup returns the peerState for target without creating one.
// Lock-free under sync.Map's read-mostly path.
func (t *peerStateTracker) lookup(target hraft.ServerAddress) *peerState {
	v, ok := t.peers.Load(target)
	if !ok {
		return nil
	}
	return v.(*peerState)
}

// getOrCreate returns the peerState for target, creating one atomically
// on first use. Used by failure paths only; success paths use lookup so
// success against a never-failed peer doesn't allocate.
func (t *peerStateTracker) getOrCreate(target hraft.ServerAddress) *peerState {
	if ps, ok := t.peers.Load(target); ok {
		return ps.(*peerState)
	}
	v, _ := t.peers.LoadOrStore(target, &peerState{})
	return v.(*peerState)
}

// consecutiveErrCount is a test-only inspector that returns the current
// consecutive-failure count for target. Exposed so tests can assert
// counters without poking unexported atomic fields directly.
func (t *peerStateTracker) consecutiveErrCount(target hraft.ServerAddress) int {
	ps := t.lookup(target)
	if ps == nil {
		return 0
	}
	return int(ps.consecutiveErr.Load())
}
