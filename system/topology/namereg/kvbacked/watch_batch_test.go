// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestStrongRejectsInvalidWatchBeforeCancellation(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}
	close(w.closed)
	run := &reconcilerLifecycle{ctx: ctx, cancel: cancel}
	run.watch.Store(&reconcilerWatch{Watcher: w})
	r.reconciler.Store(run)
	r.ready.Store(true)
	if _, err := r.RegisterScope(t.Context(), "claim", mkPID("node-1", "owner"), globalapi.Strong); !errors.Is(err, globalapi.ErrNotReady) {
		t.Fatalf("registration with invalid watch: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("test requires an owner whose cancellation worker has not run")
	}
	if _, err := r.engine.Get(pendingKey("claim")); !errors.Is(err, kvapi.ErrKeyNotFound) {
		t.Fatalf("rejected registration wrote a pending claim: %v", err)
	}
}

func TestRegistryBatchWatchLimit(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprintf("overflow=%v", overflow), func(t *testing.T) {
			r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
			r.strong.isLeader = func() bool { return false }
			core, logs := observer.New(zap.ErrorLevel)
			r.logger = zap.New(core)
			if err := r.StartReconciler(t.Context()); err != nil {
				t.Fatal(err)
			}
			owner := mkPID("node-1", "owner")
			count := systemkv.DefaultWatchLimits().MaxEvents
			if overflow {
				count++
			}
			ops := make([]kvapi.TxnOp, count)
			for i := range ops {
				name := fmt.Sprintf("batch-%d", i)
				value, err := encode(activeValue{Name: name, PID: owner.String()})
				if err != nil {
					t.Fatal(err)
				}
				ops[i] = kvapi.TxnOp{Kind: kvapi.TxnPut, Key: activeKey(name), Value: value}
			}
			if committed, err := r.engine.Txn(ops); err != nil || !committed {
				t.Fatalf("batch did not commit: %v, %v", committed, err)
			}
			if !overflow {
				if !r.NameReady() || logs.Len() != 0 {
					t.Fatal("batch at the watch limit invalidated naming")
				}
				return
			}
			if r.NameReady() {
				t.Fatal("overflow left naming admission open")
			}
			if err := r.reconciler.Load().watch.Load().Err(); !errors.Is(err, kvapi.ErrWatchOverflow) {
				t.Fatalf("watch error: %v", err)
			}
			if !eventually(t, time.Second, func() bool { return logs.Len() == 1 }) {
				t.Fatal("watch overflow was not reported")
			}
			entry := logs.All()[0]
			if entry.ContextMap()["error"] != kvapi.ErrWatchOverflow.Error() {
				t.Fatalf("missing overflow cause: %+v", entry)
			}
			if _, err := r.RegisterScope(t.Context(), "after-overflow", owner, globalapi.Strong); !errors.Is(err, globalapi.ErrNotReady) {
				t.Fatalf("post-overflow registration: %v", err)
			}
			if err := r.StartReconciler(t.Context()); err == nil {
				t.Fatal("invalidated reconciler restarted without a fresh service")
			}
		})
	}
}
