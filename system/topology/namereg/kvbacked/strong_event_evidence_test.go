// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongCommittedActiveEventOnlyResolvesMatchingAttempt(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	const first = "00000000000000000000000000000011"
	const next = "00000000000000000000000000000022"
	old := &strongWaiter{attemptID: first, ch: make(chan strongCompletion, 1)}
	replacement := &strongWaiter{attemptID: next, ch: make(chan strongCompletion, 1)}
	r.strong.addWaiter("claim", old)
	r.strong.addWaiter("claim", replacement)
	t.Cleanup(func() {
		r.strong.removeWaiter("claim", old)
		r.strong.removeWaiter("claim", replacement)
	})
	owner := mkPID("node-1", "same-pid")
	value, err := encode(activeValue{PID: owner.String(), Name: "claim", AttemptID: first, Strong: true})
	if err != nil {
		t.Fatal(err)
	}
	// The active record was deleted before the watch handler ran. Its
	// committed event must still resolve the original attempt; an unrelated
	// attempt with the same name and PID cannot inherit that evidence.
	r.handleWatchEvent(kvapi.WatchEvent{
		Type: kvapi.WatchPut, Index: 42,
		Current: &kvapi.Entry{Key: activeKey("claim"), Value: value},
	})
	select {
	case got := <-old.ch:
		if got.out.State != globalapi.RegisterStateActive || got.out.Epoch != 42 || !got.out.PID.Equal(owner) {
			t.Fatalf("committed first attempt: %+v", got)
		}
	default:
		t.Fatal("committed first attempt success was lost")
	}
	select {
	case got := <-replacement.ch:
		t.Fatalf("replacement attempt received old event: %+v", got)
	default:
	}
}

func TestStrongUnresolvedWaiterReturnsUncertaintyWhenWatchInvalidates(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1", "unavailable-peer"}, time.Minute, nil)
	r.strong.isLeader = func() bool { return false }
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	if err := r.StartReconciler(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.RegisterScope(ctx, "claim", mkPID("node-1", "owner"), globalapi.Strong)
		done <- err
	}()
	if !eventually(t, 2*time.Second, func() bool {
		_, err := r.engine.Get(pendingKey("claim"))
		return err == nil
	}) {
		t.Fatal("attempt was not committed before invalidation")
	}
	w := r.reconciler.Load().watch.Load()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, globalapi.ErrNotReady) {
			t.Fatalf("unresolved attempt after watch invalidation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("invalidated watcher stranded unresolved caller")
	}
}
