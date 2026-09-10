// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
)

type countedReconcilerEngine struct {
	kvapi.Engine
	calls atomic.Int32
}

func (e *countedReconcilerEngine) Watch(context.Context, string) (kvapi.Watcher, error) {
	e.calls.Add(1)
	return &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}, nil
}
func TestReconcilerRejectsDuplicateStartBeforeCreatingWatcher(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	engine := &countedReconcilerEngine{Engine: r.engine}
	r.engine = engine
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, r.StartReconciler(ctx))
	require.Error(t, r.StartReconciler(ctx))
	require.Equal(t, int32(1), engine.calls.Load())
	require.True(t, r.NameReady(), "rejected duplicate cannot close the current owner")
}
func TestCanceledReconcilerStartCreatesNoWatcher(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	engine := &countedReconcilerEngine{Engine: r.engine}
	r.engine = engine
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, r.StartReconciler(ctx), context.Canceled)
	require.Zero(t, engine.calls.Load())
	require.False(t, r.NameReady())
}

type blockedEventsWatcher struct {
	*readinessWatcher
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedEventsWatcher) Events() <-chan kvapi.WatchEvent {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.events
}

type blockedEventsEngine struct {
	kvapi.Engine
	watcher *blockedEventsWatcher
}

func (e *blockedEventsEngine) Watch(context.Context, string) (kvapi.Watcher, error) {
	return e.watcher, nil
}
func TestReconcilerCancellationClosesReadinessBeforeWorkerRuns(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	w := &blockedEventsWatcher{readinessWatcher: &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})}, entered: make(chan struct{}), release: make(chan struct{})}
	r.engine = &blockedEventsEngine{Engine: r.engine, watcher: w}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer close(w.release)
	require.NoError(t, r.StartReconciler(ctx))
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("watch worker did not start")
	}
	cancel()
	require.False(t, r.NameReady(), "cancellation must close admission without waiting for worker scheduling")
}

func TestReconcilerFailedSeedReleasesStartupOwnership(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	engine := &countedReconcilerEngine{Engine: r.engine}
	r.engine = engine
	ok, err := r.engine.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnPut, Key: activeKey("broken"), Value: []byte{0xc1}}})
	require.NoError(t, err)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.Error(t, r.StartReconciler(ctx))
	require.False(t, r.NameReady())
	ok, err = r.engine.Txn([]kvapi.TxnOp{{Kind: kvapi.TxnDelete, Key: activeKey("broken")}})
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, r.StartReconciler(ctx), "failed startup must release ownership after cleanup")
	require.Equal(t, int32(2), engine.calls.Load())
	require.True(t, r.NameReady())
}
