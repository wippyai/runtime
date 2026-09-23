// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

var errVoteReplyLost = errors.New("vote reply lost after commit")

func TestStrongRestartRestoresAcknowledgedRecord(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "peer"}, time.Second, nil)
	owner := mkPID("node-1", "owner")
	putPendingAttempt(t, r.engine, "restart", owner, []pid.NodeID{"node-1", "peer"})
	if _, err := r.engine.Set(ackKey("claim", "restart", "node-1"), []byte("node-1")); err != nil {
		t.Fatal(err)
	}
	fresh := NewService(r.engine, "node-1", nil, nil)
	fresh.ConfigureStrong(StrongDeps{Members: testStrongMembers, IsLeader: func() bool { return false }})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := fresh.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	if got, held := fresh.IsStrongReserved("claim"); !held || !got.Equal(owner) {
		t.Fatalf("acknowledged reservation missing after restart: owner=%v held=%v", got, held)
	}
}

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
	right := ackKey("x", attemptB, attemptA+":node")
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

func TestStrongVoteReplyLossKeepsDurableRecord(t *testing.T) {
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
		t.Fatalf("uncertain ACK response dropped record: owner=%v reserved=%v", got, ok)
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
	entry, err := base.Get(pendingKey("claim"))
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := decodePending(entry.Value)
	if err != nil {
		t.Fatal(err)
	}
	rewritten.RequiredNodes = []pid.NodeID{"node-1"}
	value, err := encode(rewritten)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := base.CompareAndSwap(entry.Key, entry.Version, value); err != nil || !ok {
		t.Fatalf("explicit rewrite: updated=%v err=%v", ok, err)
	}
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
