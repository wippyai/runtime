// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"errors"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

func TestStrongRegisterRequiresReconcilerReadiness(t *testing.T) {
	t.Run("no watcher", func(t *testing.T) {
		r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
		p := mkPID("node-1", "owner")

		_, err := r.RegisterScope(t.Context(), "claim", p, globalapi.Strong)
		if !errors.Is(err, globalapi.ErrNotReady) {
			t.Fatalf("Strong registration before reconciler startup: got %v, want ErrNotReady", err)
		}
		if _, err := r.engine.Get(pendingKey("claim")); !errors.Is(err, kvapi.ErrKeyNotFound) {
			t.Fatalf("readiness rejection wrote a pending claim: %v", err)
		}
	})

	t.Run("healthy", func(t *testing.T) {
		r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
		startStrongReconciler(t, r)

		out, err := r.RegisterScope(t.Context(), "claim", mkPID("node-1", "owner"), globalapi.Strong)
		if err != nil {
			t.Fatalf("healthy Strong registration: %v", err)
		}
		if out.State != globalapi.RegisterStateActive {
			t.Fatalf("healthy Strong registration state: %v", out.State)
		}
	})

	t.Run("watch invalidated while worker blocked", func(t *testing.T) {
		r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
		w := &invalidatingWatcher{
			blockedEventsWatcher: &blockedEventsWatcher{
				readinessWatcher: &readinessWatcher{
					events: make(chan kvapi.WatchEvent),
					closed: make(chan struct{}),
				},
				entered: make(chan struct{}),
				release: make(chan struct{}),
			},
			done: make(chan struct{}),
		}
		r.engine = &invalidatingEngine{Engine: r.engine, watcher: w}
		defer close(w.release)
		if err := r.StartReconciler(t.Context()); err != nil {
			t.Fatalf("start reconciler: %v", err)
		}
		select {
		case <-w.entered:
		case <-time.After(time.Second):
			t.Fatal("reconciler worker did not enter blocked watch read")
		}
		if !r.NameReady() {
			t.Fatal("healthy reconciler was not ready before invalidation")
		}

		close(w.done)
		_, err := r.RegisterScope(t.Context(), "claim", mkPID("node-1", "owner"), globalapi.Strong)
		if !errors.Is(err, globalapi.ErrNotReady) {
			t.Fatalf("Strong registration after watch invalidation: got %v, want ErrNotReady", err)
		}
		if _, err := r.engine.Get(pendingKey("claim")); !errors.Is(err, kvapi.ErrKeyNotFound) {
			t.Fatalf("post-invalidation rejection wrote a pending claim: %v", err)
		}
	})
}
