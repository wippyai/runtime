// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"os"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// The fixture exports Asyncify controls, a bounded allocator, and inc/exchange/grow.
// Its 1 KiB suspension stack is allocated above the value at address 1024.
// Source: testdata/process_async_reuse.wat; rebuild with wasm-tools parse.
func loadSelfContainedAsyncModule(ctx context.Context, t testing.TB) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()
	data, err := os.ReadFile("testdata/process_async_reuse.wasm")
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}

	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("wasmrt.New: %v", err)
	}

	mod, err := rt.LoadComponent(ctx, data)
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("LoadComponent: %v", err)
	}

	if err := mod.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("mod.Compile: %v", err)
	}

	return rt, mod
}

// TestProcessAsyncReuse_StateRetainedAcrossInitStep verifies that sequential Init/Step
// calls on an Asyncify-backed Process reuse the warm instance and retain both mutable
// global variables and linear memory state across calls.
func TestProcessAsyncReuse_StateRetainedAcrossInitStep(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadSelfContainedAsyncModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{AsyncifyStackBytes: 1024}, nil)
	defer p.Close()

	// 1. First execution: call "inc", expect 1
	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatalf("call 1 Init: %v", err)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("call 1 Step: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("call 1 Step status = %v, want done", out.Status())
	}
	res1 := out.Result()
	if res1 == nil {
		t.Fatal("call 1 Result is nil")
	}
	if got, ok := res1.Data().(int32); !ok || got != 1 {
		t.Fatalf("call 1 got %v (%T), want 1", res1.Data(), res1.Data())
	}

	// Verify instance was created and retained on the Asyncify scheduler path
	if p.inst == nil {
		t.Fatal("process instance was destroyed; expected warm retained instance")
	}
	if p.inst.Scheduler() == nil {
		t.Fatal("expected instance to have Asyncify scheduler enabled")
	}
	firstInst := p.inst

	// 2. Second execution: call "inc" again, expect 2 (global counter retained)
	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatalf("call 2 Init: %v", err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("call 2 Step: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("call 2 Step status = %v, want done", out.Status())
	}
	res2 := out.Result()
	if res2 == nil {
		t.Fatal("call 2 Result is nil")
	}
	if got, ok := res2.Data().(int32); !ok || got != 2 {
		t.Fatalf("call 2 got %v (%T), want 2 (global state not retained across Init/Step)", res2.Data(), res2.Data())
	}
	if p.inst != firstInst {
		t.Fatalf("instance changed (%p != %p), warm instance was not retained", p.inst, firstInst)
	}

	// 3. Third execution: write to memory offset 1024 with exchange(42), expect previous value 0
	if err := p.Init(ctx, "exchange", []payload.Payload{payload.New(int32(42))}); err != nil {
		t.Fatalf("call 3 Init: %v", err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("call 3 Step: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("call 3 Step status = %v, want done", out.Status())
	}
	if got, ok := out.Result().Data().(int32); !ok || got != 0 {
		t.Fatalf("call 3 exchange(42) got %v, want 0", got)
	}
	if p.inst != firstInst {
		t.Fatalf("instance changed on call 3 (%p != %p)", p.inst, firstInst)
	}

	// 4. Fourth execution: write to memory offset 1024 with exchange(99), expect previous value 42
	if err := p.Init(ctx, "exchange", []payload.Payload{payload.New(int32(99))}); err != nil {
		t.Fatalf("call 4 Init: %v", err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("call 4 Step: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("call 4 Step status = %v, want done", out.Status())
	}
	if got, ok := out.Result().Data().(int32); !ok || got != 42 {
		t.Fatalf("call 4 exchange(99) got %v, want 42 (linear memory not retained across Init/Step)", got)
	}
	if p.inst != firstInst {
		t.Fatalf("instance changed on call 4 (%p != %p)", p.inst, firstInst)
	}
}

// TestProcessAsyncReuse_MaxRetainedMemoryRecycling verifies that MaxRetainedMemoryBytes
// limits are enforced on the Asyncify scheduler path via resetAfterSync().
func TestProcessAsyncReuse_MaxRetainedMemoryRecycling(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadSelfContainedAsyncModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	t.Run("instance kept warm when memory within limit", func(t *testing.T) {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
			AsyncifyStackBytes:     1024,
			MaxRetainedMemoryBytes: 1024 * 1024, // 1MB > 64KB initial page
		}, nil)
		defer p.Close()

		if err := p.Init(ctx, "inc", nil); err != nil {
			t.Fatal(err)
		}
		var out process.StepOutput
		if err := p.Step(nil, &out); err != nil {
			t.Fatalf("Step error = %v", err)
		}
		if !out.IsDone() {
			t.Fatalf("status = %v, want done", out.Status())
		}
		if p.inst == nil {
			t.Fatal("warm instance prematurely discarded")
		}
	})

	t.Run("replacement requested when memory grows above limit", func(t *testing.T) {
		// Limit exactly 1 page (64KB = 65536 bytes)
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
			AsyncifyStackBytes:     1024,
			MaxRetainedMemoryBytes: 64 * 1024,
		}, nil)
		defer p.Close()

		// Call 1: inc() stays at 1 page (64KB), within limit
		if err := p.Init(ctx, "inc", nil); err != nil {
			t.Fatal(err)
		}
		var out process.StepOutput
		if err := p.Step(nil, &out); err != nil {
			t.Fatalf("call 1 Step error = %v", err)
		}
		if !out.IsDone() {
			t.Fatalf("call 1 status = %v, want done", out.Status())
		}
		if p.inst == nil {
			t.Fatal("warm instance discarded when within limit")
		}

		// Call 2: grow(1) grows memory to 2 pages (128KB > 64KB limit)
		if err := p.Init(ctx, "grow", []payload.Payload{payload.New(int32(1))}); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		err := p.Step(nil, &out)
		if !out.IsDone() {
			t.Fatalf("call 2 status = %v, want done", out.Status())
		}
		if !errors.Is(err, process.ErrProcessReplacementRequested) {
			t.Fatalf("call 2 Step error = %v, want %v", err, process.ErrProcessReplacementRequested)
		}
		// Instance is kept warm until pool replaces it
		if p.inst == nil {
			t.Fatal("process discarded instance before pool replacement")
		}
	})

	t.Run("explicit zero limit disables memory recycling", func(t *testing.T) {
		p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
			AsyncifyStackBytes:     1024,
			MaxRetainedMemoryBytes: 0,
		}, nil)
		defer p.Close()

		if err := p.Init(ctx, "grow", []payload.Payload{payload.New(int32(2))}); err != nil {
			t.Fatal(err)
		}
		var out process.StepOutput
		if err := p.Step(nil, &out); err != nil {
			t.Fatalf("Step error = %v, want nil", err)
		}
		if !out.IsDone() {
			t.Fatalf("status = %v, want done", out.Status())
		}
		if p.inst == nil {
			t.Fatal("explicit zero limit recycled instance")
		}
	})
}

// TestProcessAsyncReuse_ActorCompletionTerminates verifies that while non-actor
// Asyncify executions keep their instance warm via resetAfterSync(), actor mailbox
// completions still call endExecution() and tear down the instance.
func TestProcessAsyncReuse_ActorCompletionTerminates(t *testing.T) {
	p, self, sender := newMessagingActor(t)
	defer p.Close()

	var out process.StepOutput
	// Step 1: start actor, should park in receive waiting for mailbox message
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsIdle() {
		t.Fatalf("expected actor to idle waiting for message, got %v", out.Status())
	}
	if p.inst == nil {
		t.Fatal("actor instance must be active while running")
	}

	// Step 2: send an increment message, actor processes and replies
	pkg := relay.NewPackage(sender, self, "increment")
	event, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: pkg})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Step([]process.Event{event}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Count() != 1 {
		t.Fatalf("expected 1 yield for send, got %d", out.Count())
	}
	tag := out.Yields()[0].Tag

	// Complete the yield
	out.Reset()
	if err := p.Step([]process.Event{{Type: process.EventYieldComplete, Tag: tag, Data: process.SendResult{}}}, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsIdle() {
		t.Fatalf("expected actor to idle after send complete, got %v", out.Status())
	}
	if p.inst == nil {
		t.Fatal("actor instance should remain active while waiting for next message")
	}

	// Step 3: send "stop" message to trigger actor completion (lib.rs: if topic == "stop" return Ok(()))
	stopPkg := relay.NewPackage(sender, self, "stop")
	stopEvent, err := p.EventAdmission().AdmitEvent(process.Event{Type: process.EventMessage, Data: stopPkg})
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Step([]process.Event{stopEvent}, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsDone() {
		t.Fatalf("expected actor to be Done on stop, got %v", out.Status())
	}

	// CRITICAL: Actor completion must call endExecution() and set p.Process.inst = nil
	if p.inst != nil {
		t.Fatal("actor completion failed to tear down instance: p.Process.inst is not nil")
	}
}

// TestProcessAsyncReuse_ResetSoundness verifies that softReset cleanly resets per-call
// state (session, callArgs, started, done, cancel) without leaking or leaving stale state,
// and supports consecutive Init calls and multiple method switches on the warm instance.
func TestProcessAsyncReuse_ResetSoundness(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadSelfContainedAsyncModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{AsyncifyStackBytes: 1024}, nil)
	defer p.Close()

	// 1. Consecutive Init calls before Step should cleanly overwrite without panic or leak
	if err := p.Init(ctx, "grow", []payload.Payload{payload.New(int32(5))}); err != nil {
		t.Fatalf("initial Init: %v", err)
	}
	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatalf("overwriting Init: %v", err)
	}
	if p.method != "inc" || p.input != nil {
		t.Fatalf("Init did not overwrite per-call fields: method=%s, input=%v", p.method, p.input)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("Step after consecutive Init: %v", err)
	}
	if !out.IsDone() || out.Result() == nil || out.Result().Data().(int32) != 1 {
		t.Fatalf("unexpected result after consecutive Init: %v", out.Result())
	}

	// 2. Interleaved method calls on warm instance: inc -> exchange -> inc
	if err := p.Init(ctx, "exchange", []payload.Payload{payload.New(int32(123))}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsDone() || out.Result().Data().(int32) != 0 {
		t.Fatalf("exchange result = %v, want 0", out.Result().Data())
	}

	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	// Previous inc was 1, so this inc must be 2
	if !out.IsDone() || out.Result().Data().(int32) != 2 {
		t.Fatalf("second inc result = %v, want 2", out.Result().Data())
	}

	// 3. Verify that after Step completion, internal per-call state is cleanly wiped
	if p.session != nil {
		t.Fatal("p.session must be nil after resetAfterSync")
	}
	if p.execCtx != nil {
		t.Fatal("p.execCtx must be nil after resetAfterSync")
	}
	if p.callArgs != nil {
		t.Fatal("p.callArgs must be nil after resetAfterSync")
	}
	if p.cancel != nil {
		t.Fatal("p.cancel must be nil after resetAfterSync")
	}
	if p.started {
		t.Fatal("p.started must be false after resetAfterSync")
	}
}

// TestProcessAsyncReuse_CancelAndErrorCleanup verifies that when execution fails
// or context is canceled, endExecution() tears down the instance so no corrupted
// instance is reused, and subsequent Init/Step calls recover cleanly.
func TestProcessAsyncReuse_CancelAndErrorCleanup(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadSelfContainedAsyncModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{AsyncifyStackBytes: 1024}, nil)
	defer p.Close()

	// 1. Initial successful call establishing warm instance
	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatal(err)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}
	if !out.IsDone() || p.inst == nil {
		t.Fatal("failed to establish warm instance")
	}

	// 2. Call with pre-canceled context
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	if err := p.Init(cancelCtx, "inc", nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := p.Step(nil, &out)
	if err == nil {
		t.Fatal("expected error on canceled context, got nil")
	}

	// Canceled/failed step must invoke endExecution(), tearing down p.inst
	if p.inst != nil {
		t.Fatal("failed call did not tear down instance: p.inst is not nil")
	}

	// 3. Recovery: subsequent call with valid context instantiates fresh module
	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := p.Step(nil, &out); err != nil {
		t.Fatalf("recovery Step error: %v", err)
	}
	if !out.IsDone() {
		t.Fatalf("recovery status = %v, want done", out.Status())
	}
	// Because instance was torn down on cancel, fresh instance global starts at 1
	if got := out.Result().Data().(int32); got != 1 {
		t.Fatalf("recovered instance result = %v, want 1 for fresh instance", got)
	}
	if p.inst == nil {
		t.Fatal("recovered instance was not retained")
	}
}

// TestProcessAsyncReuse_CloseSoundness verifies that Close releases all resources
// idempotently.
func TestProcessAsyncReuse_CloseSoundness(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	rt, mod := loadSelfContainedAsyncModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{AsyncifyStackBytes: 1024}, nil)

	if err := p.Init(ctx, "inc", nil); err != nil {
		t.Fatal(err)
	}
	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		t.Fatal(err)
	}

	// Close releases instance and context
	p.Close()
	if p.inst != nil {
		t.Fatal("p.inst not nil after Close")
	}
	if p.ctx != nil {
		t.Fatal("p.ctx not nil after Close")
	}

	// Idempotent Close() call must not panic
	p.Close()
}
