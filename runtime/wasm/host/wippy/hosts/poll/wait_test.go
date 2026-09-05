// SPDX-License-Identifier: MPL-2.0

package poll

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	wippyhost "github.com/wippyai/runtime/runtime/wasm/host/wippy"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/clocks"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"github.com/wippyai/wasm-runtime/wat"
)

type signaledPollable struct {
	signal     chan struct{}
	registered chan struct{}
	ready      atomic.Bool
}

func (*signaledPollable) Type() preview2.ResourceType { return preview2.ResourcePollable }
func (p *signaledPollable) Drop()                     {}
func (p *signaledPollable) Ready() bool               { return p.ready.Load() }
func (p *signaledPollable) Block(context.Context)     { panic("worker must not block") }
func (p *signaledPollable) Notify() <-chan struct{} {
	if p.registered != nil {
		select {
		case p.registered <- struct{}{}:
		default:
		}
	}
	return p.signal
}
func (p *signaledPollable) fire() { p.ready.Store(true); close(p.signal) }

func TestPollWaitSignalsAndDuplicateIndexes(t *testing.T) {
	p := &signaledPollable{signal: make(chan struct{}), registered: make(chan struct{}, 1)}
	timer := clocks.NewDispatcherTimerPollable(time.Now().Add(time.Hour))
	w := &waitSources{sources: []preview2.Pollable{p, timer, p}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan []uint32, 1)
	go func() {
		indexes, err := w.wait(ctx)
		if err != nil {
			indexes = nil
		}
		result <- indexes
	}()
	select {
	case <-p.registered:
	case <-ctx.Done():
		t.Fatal("wait did not register")
	}
	p.fire()
	select {
	case indexes := <-result:
		require.Equal(t, []uint32{0, 2}, indexes)
	case <-ctx.Done():
		t.Fatal("lost readiness wakeup")
	}
}

func TestPollWaitTimerAndCancellation(t *testing.T) {
	p := &signaledPollable{signal: make(chan struct{})}
	timer := clocks.NewDispatcherTimerPollable(time.Now().Add(5 * time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := &waitSources{sources: []preview2.Pollable{p, timer}}
	indexes, err := w.wait(ctx)
	require.NoError(t, err)
	require.Equal(t, []uint32{1}, indexes)
	canceled, stop := context.WithCancel(context.Background())
	stop()
	w.sources = []preview2.Pollable{p}
	_, err = w.wait(canceled)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPollValidatesEntireInputAndSuspends(t *testing.T) {
	table := preview2.NewResourceTable()
	host := NewHost(table)
	ready := &signaledPollable{signal: make(chan struct{})}
	ready.fire()
	h := table.Add(ready)
	require.Panics(t, func() { host.Poll(context.Background(), nil) })
	require.Panics(t, func() { host.Poll(context.Background(), []uint32{h, h + 999}) })
	require.Panics(t, func() { host.MethodPollableReady(context.Background(), h+999) })
	require.Panics(t, func() { host.ResourceDropPollable(context.Background(), h+999) })
	require.Equal(t, []uint32{0, 1}, host.Poll(context.Background(), []uint32{h, h}))
	waiting := table.Add(&signaledPollable{signal: make(chan struct{})})
	ctx, async := newAsyncContext()
	require.Nil(t, host.Poll(ctx, []uint32{waiting}))
	require.True(t, async.IsUnwinding(ctx))
	require.Error(t, validatePollArguments(context.Background(), nil, []uint64{0, 4097, 0}))
}

func TestPollRewindConsumesDispatcherResultOnce(t *testing.T) {
	ctx, async := newAsyncContext()
	scheduler := wasmengine.NewScheduler(async)
	ctx = wasmengine.WithScheduler(ctx, scheduler)
	store := wippyhost.NewAsyncValueStore()
	ctx = wippyhost.WithAsyncValueStore(ctx, store)
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	code, err := wat.Compile(`(module (func (export "noop")))`)
	require.NoError(t, err)
	mod, err := rt.Instantiate(ctx, code)
	require.NoError(t, err)
	require.NoError(t, scheduler.Execute(ctx, mod.ExportedFunction("noop")))
	token := store.Put([]uint32{1, 3})
	_, err = scheduler.Step(ctx, &wasmengine.YieldResult{Value: token})
	require.Error(t, err) // no-op fixture has no Asyncify exports
	require.True(t, async.IsRewinding(ctx))
	require.Equal(t, []uint32{1, 3}, NewHost(nil).Poll(ctx, nil))
	require.True(t, async.IsNormal(ctx))
	_, ok := store.Take(token)
	require.False(t, ok)
}

func TestPollWaitWakesWhenResourceScopeCloses(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(8, 1)
	p := &preview2.PollableResource{}
	table.Add(p)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan []uint32, 1)
	w := &waitSources{sources: []preview2.Pollable{p}}
	go func() {
		indexes, err := w.wait(ctx)
		if err != nil {
			indexes = nil
		}
		done <- indexes
	}()
	require.NoError(t, table.Close())
	select {
	case indexes := <-done:
		require.Equal(t, []uint32{0}, indexes)
	case <-ctx.Done():
		t.Fatal("scope close did not wake poll")
	}
}
