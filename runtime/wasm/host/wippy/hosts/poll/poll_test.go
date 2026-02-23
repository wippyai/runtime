// SPDX-License-Identifier: MPL-2.0

package poll

import (
	"context"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/clocks"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

type mockPollDispatcher struct {
	lastCmd     dispatcher.Command
	completeDur time.Duration
	calls       int
}

func (m *mockPollDispatcher) Dispatch(cmd dispatcher.Command) dispatcher.Handler {
	m.calls++
	m.lastCmd = cmd
	return dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		sleepDur := m.completeDur
		if sleepDur <= 0 {
			if sleepCmd, ok := cmd.(clockapi.SleepCmd); ok {
				sleepDur = sleepCmd.Duration
			}
		}
		if sleepDur <= 0 {
			receiver.CompleteYield(tag, nil, nil)
			return nil
		}
		time.AfterFunc(sleepDur, func() {
			receiver.CompleteYield(tag, nil, nil)
		})
		return nil
	})
}

func TestHost_RegisterAndAsyncFunctions(t *testing.T) {
	host := NewHost(preview2.NewResourceTable())
	funcs := host.Register()

	if _, ok := funcs["poll"]; !ok {
		t.Fatal("Register() missing poll")
	}
	if _, ok := funcs["[method]pollable.ready"]; !ok {
		t.Fatal("Register() missing pollable.ready")
	}
	if _, ok := funcs["[method]pollable.block"]; !ok {
		t.Fatal("Register() missing pollable.block")
	}
	if _, ok := funcs["[resource-drop]pollable"]; !ok {
		t.Fatal("Register() missing pollable drop")
	}

	async := host.AsyncFunctions()
	if len(async) != 1 || async[0] != "[method]pollable.block" {
		t.Fatalf("AsyncFunctions() = %#v, want [\"[method]pollable.block\"]", async)
	}
}

func TestHost_BlockAndDrop(t *testing.T) {
	resources := preview2.NewResourceTable()
	mockDisp := &mockPollDispatcher{}
	mono := clocks.NewMonotonicClockHost(resources)
	host := NewHost(resources)

	handle := mono.SubscribeDuration(context.Background(), uint64(8*time.Millisecond))
	if handle == 0 {
		t.Fatal("SubscribeDuration() returned zero handle")
	}

	if host.MethodPollableReady(context.Background(), handle) {
		t.Fatal("pollable should not be ready immediately")
	}

	ctx, async := newAsyncContext()

	// First call should suspend and mark asyncify as unwinding.
	host.MethodPollableBlock(ctx, handle)
	if !async.IsUnwinding(ctx) {
		t.Fatal("MethodPollableBlock() should suspend via asyncify unwind")
	}

	// Host call should not dispatch synchronously; scheduler dispatches yielded commands.
	if mockDisp.calls != 0 {
		t.Fatalf("dispatcher calls = %d, want 0 during host call", mockDisp.calls)
	}

	// Simulate scheduler transition into rewind and second host call path.
	if err := async.StopUnwind(ctx); err != nil {
		t.Fatalf("StopUnwind() error = %v", err)
	}
	if err := async.StartRewind(ctx); err != nil {
		t.Fatalf("StartRewind() error = %v", err)
	}
	host.MethodPollableBlock(ctx, handle)
	if !async.IsNormal(ctx) {
		t.Fatal("MethodPollableBlock() rewind path should restore asyncify normal state")
	}

	host.ResourceDropPollable(context.Background(), handle)
	if host.MethodPollableReady(context.Background(), handle) {
		t.Fatal("pollable should be removed after drop")
	}
}

func TestHost_BlockRewindWhenAlreadyReady(t *testing.T) {
	resources := preview2.NewResourceTable()
	mono := clocks.NewMonotonicClockHost(resources)
	host := NewHost(resources)

	handle := mono.SubscribeDuration(context.Background(), uint64(2*time.Millisecond))
	if handle == 0 {
		t.Fatal("SubscribeDuration() returned zero handle")
	}

	ctx, async := newAsyncContext()
	host.MethodPollableBlock(ctx, handle)
	if !async.IsUnwinding(ctx) {
		t.Fatal("MethodPollableBlock() should suspend via asyncify unwind")
	}

	if err := async.StopUnwind(ctx); err != nil {
		t.Fatalf("StopUnwind() error = %v", err)
	}
	if err := async.StartRewind(ctx); err != nil {
		t.Fatalf("StartRewind() error = %v", err)
	}

	// The timer can become ready before the rewound host call executes.
	time.Sleep(5 * time.Millisecond)
	host.MethodPollableBlock(ctx, handle)
	if !async.IsNormal(ctx) {
		t.Fatal("MethodPollableBlock() should resume even when pollable is already ready")
	}
}

func TestHost_BlockPanicsWithoutAsyncContext(t *testing.T) {
	resources := preview2.NewResourceTable()
	mono := clocks.NewMonotonicClockHost(resources)
	host := NewHost(resources)

	handle := mono.SubscribeDuration(context.Background(), uint64(5*time.Millisecond))
	if handle == 0 {
		t.Fatal("SubscribeDuration() returned zero handle")
	}

	defer func() {
		if recover() == nil {
			t.Fatal("MethodPollableBlock() should panic without async context")
		}
	}()
	host.MethodPollableBlock(context.Background(), handle)
}

func TestHost_BlockDoesNotDispatchSynchronously(t *testing.T) {
	resources := preview2.NewResourceTable()
	mockDisp := &mockPollDispatcher{completeDur: time.Millisecond}
	mono := clocks.NewMonotonicClockHost(resources)
	host := NewHost(resources)

	handle := mono.SubscribeDuration(context.Background(), uint64(50*time.Millisecond))
	if handle == 0 {
		t.Fatal("SubscribeDuration() returned zero handle")
	}

	ctx, _ := newAsyncContext()
	host.MethodPollableBlock(ctx, handle)
	if mockDisp.calls != 0 {
		t.Fatalf("dispatcher calls = %d, want 0 (scheduler dispatches yielded ops)", mockDisp.calls)
	}
}

func newAsyncContext() (context.Context, *wasmengine.Asyncify) {
	async := wasmengine.NewAsyncify()
	scheduler := wasmengine.NewScheduler(async)
	ctx := context.Background()
	ctx = wasmengine.WithAsyncify(ctx, async)
	ctx = wasmengine.WithScheduler(ctx, scheduler)
	return ctx, async
}

var _ dispatcher.Dispatcher = (*mockPollDispatcher)(nil)
