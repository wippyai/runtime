// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

var errVoteReplyLost = errors.New("vote reply lost after commit")

// voteGateEngine pauses the exact vote transaction, rather than a preceding
// read. This makes replacement and terminal races land at the transaction
// boundary where the pending-version guard must decide the outcome.
type voteGateEngine struct {
	kvapi.Engine
	match   func([]kvapi.TxnOp) bool
	before  func()
	entered chan struct{}
	release chan struct{}
	gateMu  sync.Mutex
	gated   bool
}

type uncertainVoteEngine struct{ kvapi.Engine }

func (e *uncertainVoteEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	committed, err := e.Engine.Txn(ops)
	if committed {
		return false, errVoteReplyLost
	}
	return committed, err
}

type pauseAfterCommitEngine struct {
	kvapi.Engine
	match   func([]kvapi.TxnOp) bool
	entered chan struct{}
	release chan struct{}
	mu      sync.Mutex
	paused  bool
}

func (e *pauseAfterCommitEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	if !e.match(ops) {
		return e.Engine.Txn(ops)
	}
	e.mu.Lock()
	first := !e.paused
	if first {
		e.paused = true
	}
	e.mu.Unlock()
	committed, err := e.Engine.Txn(ops)
	if first {
		close(e.entered)
		<-e.release
	}
	return committed, err
}

func (e *voteGateEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	if e.match(ops) {
		e.gateMu.Lock()
		first := !e.gated
		if first {
			e.gated = true
		}
		e.gateMu.Unlock()
		if first {
			if e.before != nil {
				e.before()
			}
			close(e.entered)
			<-e.release
		}
	}
	return e.Engine.Txn(ops)
}

func voteTxnFor(key string) func([]kvapi.TxnOp) bool {
	return func(ops []kvapi.TxnOp) bool {
		for _, op := range ops {
			if op.Kind == kvapi.TxnPut && op.Key == key {
				return true
			}
		}
		return false
	}
}

func TestStrongVoteKeyEscapesColonBearingComponents(t *testing.T) {
	attemptA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	attemptB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	left := ackKey("x:"+attemptB, attemptA, "node")
	right := ackKey("x", attemptB, pid.NodeID(attemptA+":node"))
	if left == right {
		t.Fatalf("vote key components alias: %q", left)
	}
	if got := ackKey("svc", attemptA, "node"); got != ackPrefix+"svc:"+attemptA+":node" {
		t.Fatalf("simple vote key changed shape: %q", got)
	}
	if got := ackKey("svc:name", attemptA, "node:1"); got != ackPrefix+"svc%3Aname:"+attemptA+":node%3A1" {
		t.Fatalf("escaped vote key=%q", got)
	}
}

func TestStrongAckAndNackCannotBothCommit(t *testing.T) {
	for _, firstReject := range []bool{false, true} {
		name := "ack-first"
		if firstReject {
			name = "nack-first"
		}
		t.Run(name, func(t *testing.T) {
			conflictOwner := mkPID("node-1", "existing")
			var calls atomic.Int32
			firstConflictReady := make(chan struct{})
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, func(string, pid.PID) (pid.PID, bool) {
				call := calls.Add(1)
				if call == 1 {
					close(firstConflictReady)
				}
				if (call == 1) == firstReject {
					return conflictOwner, true
				}
				return pid.PID{}, false
			})
			base := r.engine
			pending := putPendingAttempt(t, base, "contended", mkPID("node-1", "owner"), []pid.NodeID{"node-1"})
			ack := ackKey("claim", "contended", "node-1")
			reject := rejectKey("claim", "contended", "node-1")
			entered := make(chan struct{})
			release := make(chan struct{})
			r.engine = &voteGateEngine{Engine: base, match: func(ops []kvapi.TxnOp) bool {
				for _, op := range ops {
					if op.Kind == kvapi.TxnPut && (op.Key == ack || op.Key == reject) {
						return true
					}
				}
				return false
			}, entered: entered, release: release}
			firstDone := make(chan struct{})
			secondDone := make(chan struct{})
			go func() {
				r.strong.attest("claim", pending.Epoch, pending.Version, "contended", mkPID("node-1", "owner"), []pid.NodeID{"node-1"})
				close(firstDone)
			}()
			select {
			case <-firstConflictReady:
			case <-time.After(time.Second):
				t.Fatal("first production vote did not evaluate conflict")
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("first production vote did not reach transaction gate")
			}
			go func() {
				r.strong.attest("claim", pending.Epoch, pending.Version, "contended", mkPID("node-1", "owner"), []pid.NodeID{"node-1"})
				close(secondDone)
			}()
			select {
			case <-secondDone:
			case <-time.After(time.Second):
				t.Fatal("second production vote did not complete")
			}
			close(release)
			select {
			case <-firstDone:
			case <-time.After(time.Second):
				t.Fatal("first production vote did not complete")
			}
			if firstReject {
				if _, err := base.Get(ack); err != nil {
					t.Fatalf("ACK winner missing: %v", err)
				}
				if _, err := base.Get(reject); err == nil {
					t.Fatal("both production votes became durable")
				}
			} else {
				if _, err := base.Get(reject); err != nil {
					t.Fatalf("NACK winner missing: %v", err)
				}
				if _, err := base.Get(ack); err == nil {
					t.Fatal("both production votes became durable")
				}
			}
		})
	}
}

func putPendingAttempt(t *testing.T, engine kvapi.Engine, attempt string, owner pid.PID, required []pid.NodeID) kvapi.Entry {
	t.Helper()
	value, err := encode(pendingHeader{
		PID: owner.String(), Name: "claim", AttemptID: attempt,
		RequiredNodes: required, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Set(pendingKey("claim"), value); err != nil {
		t.Fatal(err)
	}
	entry, err := engine.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestStrongOldAttemptVoteCannotCrossPendingReplacementAtTxn(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	base := r.engine
	owner := mkPID("node-1", "old")
	old := putPendingAttempt(t, base, "old", owner, []pid.NodeID{"node-1"})
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r.engine = &voteGateEngine{
		Engine:  base,
		match:   voteTxnFor(ackKey("claim", "old", "node-1")),
		entered: entered,
		release: release,
		before: func() {
			if err := base.Delete(pendingKey("claim")); err != nil {
				t.Fatal(err)
			}
			putPendingAttempt(t, base, "replacement", mkPID("node-1", "new"), []pid.NodeID{"node-1"})
		},
	}
	done := make(chan struct{})
	go func() {
		r.strong.attest("claim", old.Epoch, old.Version, "old", owner, []pid.NodeID{"node-1"})
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("old vote did not reach transaction gate")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old vote did not finish")
	}
	if _, err := base.Get(ackKey("claim", "old", "node-1")); err == nil {
		t.Fatal("old attempt vote crossed a replacement pending version")
	}
	if _, err := base.Get(ackKey("claim", "replacement", "node-1")); err == nil {
		t.Fatal("old vote was recorded under the replacement attempt")
	}
	if _, err := base.Get(pendingKey("claim")); err != nil {
		t.Fatalf("replacement pending was lost: %v", err)
	}
}

func TestStrongVoteRetryUsesRefreshedSameAttemptVersion(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	base := r.engine
	owner := mkPID("node-1", "owner")
	old := putPendingAttempt(t, base, "stable", owner, []pid.NodeID{"node-1"})
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r.engine = &voteGateEngine{
		Engine:  base,
		match:   voteTxnFor(ackKey("claim", "stable", "node-1")),
		entered: entered,
		release: release,
		before: func() {
			hdr, err := decodePending(old.Value)
			if err != nil {
				t.Fatal(err)
			}
			hdr.CreatedAt++
			value, err := encode(hdr)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := base.Set(pendingKey("claim"), value); err != nil {
				t.Fatal(err)
			}
		},
	}
	oldDone := make(chan struct{})
	go func() {
		r.strong.attest("claim", old.Epoch, old.Version, "stable", owner, []pid.NodeID{"node-1"})
		close(oldDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("vote did not reach transaction gate")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("stale vote did not finish")
	}
	if _, err := base.Get(ackKey("claim", "stable", "node-1")); err == nil {
		t.Fatal("stale vote committed after same-attempt header rewrite")
	}
	refreshed, err := base.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Version == old.Version {
		t.Fatal("same-attempt header rewrite did not advance pending version")
	}
	hdr, err := decodePending(refreshed.Value)
	if err != nil {
		t.Fatal(err)
	}
	r.strong.attest("claim", refreshed.Epoch, refreshed.Version, hdr.AttemptID, owner, hdr.RequiredNodes)
	if _, err := base.Get(ackKey("claim", "stable", "node-1")); err != nil {
		t.Fatalf("retry on refreshed pending version did not commit: %v", err)
	}
}

func TestStrongDurableNackFastPathPreservesConflictOutcome(t *testing.T) {
	conflictOwner := mkPID("node-1", "existing")
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, func(string, pid.PID) (pid.PID, bool) {
		return conflictOwner, true
	})
	base := r.engine
	owner := mkPID("node-1", "pending")
	pending := putPendingAttempt(t, base, "nack-race", owner, []pid.NodeID{"node-1"})
	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "nack-race", pid: owner}
	r.strong.addWaiter("claim", waiter)
	t.Cleanup(func() { r.strong.removeWaiter("claim", waiter) })
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	reject := rejectKey("claim", "nack-race", "node-1")
	r.engine = &pauseAfterCommitEngine{
		Engine:  base,
		match:   voteTxnFor(reject),
		entered: entered,
		release: release,
	}
	firstDone := make(chan struct{})
	go func() {
		r.strong.attest("claim", pending.Epoch, pending.Version, "nack-race", owner, []pid.NodeID{"node-1"})
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first NACK did not pause after its durable commit")
	}
	// The second production attestation observes the durable NACK while the
	// original transaction response is still paused. It must restore the typed
	// conflict detail before terminal delivery.
	r.strong.attest("claim", pending.Epoch, pending.Version, "nack-race", owner, []pid.NodeID{"node-1"})
	if err := base.Delete(pendingKey("claim")); err != nil {
		t.Fatal(err)
	}
	r.strong.onTerminal("claim", "nack-race")
	select {
	case out := <-waiter.ch:
		_, err := r.strong.finalize("claim", owner, "nack-race", out)
		var conflict *globalapi.StrongConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("terminal outcome lost StrongConflictError: out=%+v err=%v", out, err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not receive terminal outcome")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("first NACK did not resume")
	}
}

func TestStrongVoteReplyLossKeepsDurableExclusion(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	base := r.engine
	r.engine = &uncertainVoteEngine{Engine: base}
	owner := mkPID("node-1", "owner")
	pending := putPendingAttempt(t, base, "uncertain", owner, []pid.NodeID{"node-1"})
	r.strong.attest("claim", pending.Epoch, pending.Version, "uncertain", owner, []pid.NodeID{"node-1"})
	if _, err := base.Get(ackKey("claim", "uncertain", "node-1")); err != nil {
		t.Fatalf("durable ACK missing after uncertain response: %v", err)
	}
	if got, ok := r.IsStrongReserved("claim"); !ok || !got.Equal(owner) {
		t.Fatalf("uncertain ACK response dropped exclusion: owner=%v reserved=%v", got, ok)
	}
}

func TestStrongOldAttemptNackCannotCrossTerminalAtTxn(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, func(string, pid.PID) (pid.PID, bool) {
		return mkPID("node-1", "conflict"), true
	})
	base := r.engine
	owner := mkPID("node-1", "old")
	old := putPendingAttempt(t, base, "old", owner, []pid.NodeID{"node-1"})
	waiter := &strongWaiter{ch: make(chan globalapi.RegisterOutcome, 1), attemptID: "old", pid: owner}
	r.strong.addWaiter("claim", waiter)
	t.Cleanup(func() { r.strong.removeWaiter("claim", waiter) })
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r.engine = &voteGateEngine{
		Engine:  base,
		match:   voteTxnFor(rejectKey("claim", "old", "node-1")),
		entered: entered,
		release: release,
		before: func() {
			if err := base.Delete(pendingKey("claim")); err != nil {
				t.Fatal(err)
			}
		},
	}
	done := make(chan struct{})
	go func() {
		r.strong.attest("claim", old.Epoch, old.Version, "old", owner, []pid.NodeID{"node-1"})
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("old NACK did not reach transaction gate")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old NACK did not finish")
	}
	if _, err := base.Get(rejectKey("claim", "old", "node-1")); err == nil {
		t.Fatal("old attempt NACK crossed terminal pending deletion")
	}
	r.strong.mu.Lock()
	defer r.strong.mu.Unlock()
	if len(r.strong.terminalReason) != 0 {
		t.Fatal("failed old NACK left terminal evidence behind")
	}
}

func TestStrongRejectedAttemptDoesNotAdvertiseExclusion(t *testing.T) {
	conflictOwner := mkPID("node-1", "existing")
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, func(string, pid.PID) (pid.PID, bool) {
		return conflictOwner, true
	})
	base := r.engine
	pendingOwner := mkPID("node-1", "pending")
	pending := putPendingAttempt(t, base, "rejected", pendingOwner, []pid.NodeID{"node-1"})
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	r.engine = &voteGateEngine{
		Engine:  base,
		match:   voteTxnFor(rejectKey("claim", "rejected", "node-1")),
		entered: entered,
		release: release,
	}
	done := make(chan struct{})
	go func() {
		r.strong.attest("claim", pending.Epoch, pending.Version, "rejected", pendingOwner, []pid.NodeID{"node-1"})
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("NACK did not reach transaction gate")
	}
	if got, ok := r.IsStrongReserved("claim"); ok {
		t.Fatalf("rejected pending advertised the wrong owner: got=%v want none", got)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("NACK did not finish")
	}
	if _, err := base.Get(rejectKey("claim", "rejected", "node-1")); err != nil {
		t.Fatalf("NACK was not durable: %v", err)
	}
}

func TestStrongEqualEpochReplacementDoesNotReuseVotes(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	base := r.engine
	old := putPendingAttempt(t, base, "old", mkPID("node-1", "old"), []pid.NodeID{"node-1"})
	if old.Epoch != 0 {
		t.Fatalf("local engine should expose zero raft epoch, got %d", old.Epoch)
	}
	if _, err := base.Set(ackKey("claim", "old", "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	if err := base.Delete(pendingKey("claim")); err != nil {
		t.Fatal(err)
	}
	replacement := putPendingAttempt(t, base, "new", mkPID("node-1", "new"), []pid.NodeID{"node-1"})
	if replacement.Epoch != old.Epoch {
		t.Fatalf("replacement changed local epoch: old=%d new=%d", old.Epoch, replacement.Epoch)
	}
	hdr, err := decodePending(replacement.Value)
	if err != nil {
		t.Fatal(err)
	}
	r.strong.leaderDrive("claim", replacement.Epoch, replacement.Version, hdr)
	if _, err := base.Get(activeKey("claim")); err == nil {
		t.Fatal("old attempt vote promoted an equal-epoch replacement")
	}
	if _, err := base.Get(ackKey("claim", "old", "node-1")); err != nil {
		t.Fatalf("old attempt evidence unexpectedly disappeared: %v", err)
	}
}

func TestStrongMembershipRewriteKeepsAttemptEvidence(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	base := r.engine
	owner := mkPID("node-1", "owner")
	r.strong.isLeader = func() bool { return false }
	putPendingAttempt(t, base, "stable", owner, []pid.NodeID{"node-1", "peer"})
	if _, err := base.Set(ackKey("claim", "stable", "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	r.strong.dropNodeFromPending("claim", "peer")
	updated, err := base.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	hdr, err := decodePending(updated.Value)
	if err != nil {
		t.Fatal(err)
	}
	if hdr.AttemptID != "stable" {
		t.Fatalf("membership rewrite changed attempt: %q", hdr.AttemptID)
	}
	if _, err := base.Get(ackKey("claim", "stable", "node-1")); err != nil {
		t.Fatalf("membership rewrite discarded valid vote: %v", err)
	}
	r.strong.isLeader = func() bool { return true }
	r.strong.leaderDrive("claim", updated.Epoch, updated.Version, hdr)
	if _, err := base.Get(activeKey("claim")); err != nil {
		t.Fatalf("stable vote did not promote after membership rewrite: %v", err)
	}
}

func TestStrongDelayedPendingLatchCannotOverwriteActiveEpochZero(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	activeOwner := mkPID("node-1", "active")
	staleOwner := mkPID("node-1", "stale")
	r.strong.onActive("claim", 0, "active", activeOwner)
	r.strong.latch("claim", staleOwner, "stale", 0)
	if got, ok := r.IsStrongReserved("claim"); !ok || !got.Equal(activeOwner) {
		t.Fatalf("delayed Epoch-0 pending latch replaced active owner: got=%v reserved=%v", got, ok)
	}
}
