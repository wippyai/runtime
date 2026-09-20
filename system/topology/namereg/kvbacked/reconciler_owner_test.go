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
	systemkv "github.com/wippyai/runtime/system/kv"
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

type invalidatingWatcher struct {
	*blockedEventsWatcher
	done chan struct{}
}

func (w *invalidatingWatcher) Done() <-chan struct{} { return w.done }

type invalidatingEngine struct {
	kvapi.Engine
	watcher *invalidatingWatcher
}

func (e *invalidatingEngine) Watch(context.Context, string) (kvapi.Watcher, error) {
	return e.watcher, nil
}

func TestReconcilerWatchInvalidationClosesAdmissionWhileWorkerBlocked(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	w := &invalidatingWatcher{
		blockedEventsWatcher: &blockedEventsWatcher{
			readinessWatcher: &readinessWatcher{events: make(chan kvapi.WatchEvent), closed: make(chan struct{})},
			entered:          make(chan struct{}),
			release:          make(chan struct{}),
		},
		done: make(chan struct{}),
	}
	r.engine = &invalidatingEngine{Engine: r.engine, watcher: w}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer close(w.release)
	require.NoError(t, r.StartReconciler(ctx))
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("reconciler worker did not enter blocked watch read")
	}
	close(w.done)
	require.False(t, r.NameReady(), "invalidated watch must close admission before worker runs")
}

type blockedOwnedWatch struct {
	kvapi.Watcher
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedOwnedWatch) Events() <-chan kvapi.WatchEvent {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.Watcher.Events()
}

type blockedOwnedEngine struct {
	kvapi.Engine
	watcher          *blockedOwnedWatch
	entered, release chan struct{}
}

func (e *blockedOwnedEngine) Watch(ctx context.Context, prefix string) (kvapi.Watcher, error) {
	w, err := e.Engine.Watch(ctx, prefix)
	if err != nil {
		return nil, err
	}
	e.watcher = &blockedOwnedWatch{Watcher: w, entered: e.entered, release: e.release}
	return e.watcher, nil
}

func TestReconcilerRealKVOverflowClosesAdmissionBeforeWorkerRuns(t *testing.T) {
	r := newStrongReg(t, []pid.NodeID{"node-1"}, time.Second, nil)
	eng := r.engine.(*systemkv.Service)
	require.NoError(t, eng.SetWatchLimits(systemkv.WatchLimits{
		MaxSubscriptions: 1, MaxEvents: 1, MaxBytes: 4096,
	}))
	wrapper := &blockedOwnedEngine{Engine: eng, entered: make(chan struct{}), release: make(chan struct{})}
	r.engine = wrapper
	defer close(wrapper.release)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, r.StartReconciler(ctx))
	select {
	case <-wrapper.entered:
	case <-time.After(time.Second):
		t.Fatal("reconciler did not reach the blocked delivery worker")
	}
	require.True(t, r.NameReady())
	for _, name := range []string{"first", "second"} {
		_, err := eng.Set(activeKey(name), []byte(name))
		require.NoError(t, err)
	}
	require.ErrorIs(t, wrapper.watcher.Err(), kvapi.ErrWatchOverflow)
	require.False(t, r.NameReady(), "overflow must close admission without waiting for the observer")
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
