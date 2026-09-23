// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

type heldVoteEngine struct {
	kvapi.Engine
	entered     chan struct{}
	release     chan struct{}
	key         string
	calls       atomic.Int32
	once        atomic.Bool
	releaseOnce sync.Once
}

type flakyVoteEngine struct {
	kvapi.Engine
	key   string
	calls atomic.Int32
}

func (e *flakyVoteEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == e.key && e.calls.Add(1) == 1 {
			return false, errors.New("transient vote failure")
		}
	}
	return e.Engine.Txn(ops)
}

func (e *heldVoteEngine) Release() { e.releaseOnce.Do(func() { close(e.release) }) }

func (e *heldVoteEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	for _, op := range ops {
		if op.Kind == kvapi.TxnPut && op.Key == e.key {
			e.calls.Add(1)
			if e.once.CompareAndSwap(false, true) {
				close(e.entered)
				<-e.release
			}
		}
	}
	return e.Engine.Txn(ops)
}

func TestStrongSlowVoteDoesNotBlockUnrelatedCommittedResult(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	r.strong.isLeader = func() bool { return false }
	base := r.engine
	const slowAttempt = "00000000000000000000000000000011"
	const fastAttempt = "00000000000000000000000000000022"
	held := &heldVoteEngine{
		Engine: base, key: ackKey("slow", slowAttempt, "node-1"),
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	r.engine = held
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	t.Cleanup(held.Release)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	fast := &strongWaiter{attemptID: fastAttempt, ch: make(chan strongCompletion, 1)}
	r.strong.addWaiter("fast", fast)
	t.Cleanup(func() { r.strong.removeWaiter("fast", fast) })
	owner := mkPID("node-1", "slow-owner")
	hdr, err := encode(pendingHeader{
		PID: owner.String(), Name: "slow", AttemptID: slowAttempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(pendingKey("slow"), hdr); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("slow vote did not enter blocked Raft operation")
	}
	result, err := encode(terminalResult{
		Name: "fast", AttemptID: fastAttempt, Reason: strongOwnerConflict,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(resultKey("fast", fastAttempt), result); err != nil {
		t.Fatal(err)
	}
	select {
	case completion := <-fast.ch:
		if completion.terminal == nil || completion.terminal.AttemptID != fastAttempt ||
			completion.out.State != globalapi.RegisterStateExpired {
			t.Fatalf("unrelated terminal event: %+v", completion)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("blocked Raft vote stalled unrelated committed result delivery")
	}
}

func TestStrongSlowVoteDoesNotBlockSeed(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	r.strong.isLeader = func() bool { return false }
	base := r.engine
	const attempt = "00000000000000000000000000000044"
	owner := mkPID("node-1", "seed-owner")
	hdr, err := encode(pendingHeader{
		PID: owner.String(), Name: "seed-slow", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(pendingKey("seed-slow"), hdr); err != nil {
		t.Fatal(err)
	}
	held := &heldVoteEngine{
		Engine: base, key: ackKey("seed-slow", attempt, "node-1"),
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	r.engine = held
	t.Cleanup(held.Release)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	started := time.Now()
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("seed waited for blocked vote action: %v", elapsed)
	}
	select {
	case <-held.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("seeded pending did not reach the bounded action worker")
	}
}

func TestStrongOwnerCoalescesRepeatedUpdatesPerLiveAttempt(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Minute, nil)
	o := newReconcilerOwner(r, &reconcilerLifecycle{ctx: t.Context()})
	for i := 0; i < strongReconcileWorkers*16; i++ {
		o.markAttempt("churn", "attempt")
	}
	if got := len(o.slots); got != 1 {
		t.Fatalf("one live attempt retained %d slots", got)
	}
	if got := o.ready.Len(); got != 1 {
		t.Fatalf("one live attempt queued %d times", got)
	}
	o.retire("churn", "attempt")
	o.dispatch()
	if len(o.slots) != 0 || o.ready.Len() != 0 {
		t.Fatal("retired attempt retained in owner")
	}
}

func TestStrongFailedFollowerVoteRetriesThroughOwner(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	r.strong.isLeader = func() bool { return false }
	base := r.engine
	const attempt = "00000000000000000000000000000055"
	flaky := &flakyVoteEngine{Engine: base, key: ackKey("retry", attempt, "node-1")}
	r.engine = flaky
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	owner := mkPID("node-1", "retry-owner")
	hdr, err := encode(pendingHeader{PID: owner.String(), Name: "retry", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(pendingKey("retry"), hdr); err != nil {
		t.Fatal(err)
	}
	if !eventually(t, 3*time.Second, func() bool {
		_, err := base.Get(ackKey("retry", attempt, "node-1"))
		return err == nil && flaky.calls.Load() >= 2
	}) {
		t.Fatalf("failed follower vote was not retried: calls=%d", flaky.calls.Load())
	}
}

func mustEncodeActive(t *testing.T, active activeValue) []byte {
	t.Helper()
	value, err := encode(active)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestStrongSameNameEventsCoalesceBehindBlockedAction(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "ghost"}, time.Minute, nil)
	r.strong.isLeader = func() bool { return false }
	base := r.engine
	const attempt = "00000000000000000000000000000033"
	held := &heldVoteEngine{
		Engine: base, key: ackKey("same", attempt, "node-1"),
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	r.engine = held
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	t.Cleanup(held.Release)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	owner := mkPID("node-1", "same-owner")
	hdr, err := encode(pendingHeader{
		PID: owner.String(), Name: "same", AttemptID: attempt,
		RequiredNodes: []pid.NodeID{"node-1", "ghost"}, DeadlineUnixNano: time.Now().Add(time.Minute).UnixNano(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Set(pendingKey("same"), hdr); err != nil {
		t.Fatal(err)
	}
	select {
	case <-held.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("same-name action did not enter blocked Raft operation")
	}
	// A rewrite while the action is parked must set one dirty bit, not enqueue
	// another same-name action that could run concurrently.
	if _, err := base.Set(pendingKey("same"), hdr); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := held.calls.Load(); got != 1 {
		t.Fatalf("same-name action was duplicated while blocked: %d calls", got)
	}
	held.Release()
	if !eventually(t, 2*time.Second, func() bool {
		_, err := base.Get(ackKey("same", attempt, "node-1"))
		return err == nil && held.calls.Load() >= 2
	}) {
		t.Fatalf("rewritten pending version was not retried: %d vote attempts", held.calls.Load())
	}
}
