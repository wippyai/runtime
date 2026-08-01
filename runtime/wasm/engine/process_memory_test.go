// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"errors"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

func compileTestModule(ctx context.Context, t *testing.T, source string) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()

	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("wasm runtime New() error = %v", err)
	}

	mod, err := rt.LoadWAT(ctx, source, "grow: func() -> s32;")
	if err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("LoadWAT() error = %v", err)
	}
	if err := mod.Compile(ctx); err != nil {
		_ = rt.Close(ctx)
		t.Fatalf("Compile() error = %v", err)
	}

	return rt, mod
}

func compileMemoryGrowthModule(ctx context.Context, t *testing.T) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()
	return compileTestModule(ctx, t, `(module
		(memory 1)
		(func (export "grow") (result i32)
			i32.const 2
			memory.grow
			drop
			memory.size
		)
	)`)
}

func runGrowthProcess(t *testing.T, p *Process) error {
	t.Helper()

	if err := p.Init(ctxapi.NewRootContext(), "grow", nil); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	var out process.StepOutput
	err := p.Step(nil, &out)
	if !out.IsDone() {
		t.Fatalf("Step() status = %v, want done", out.Status())
	}
	return err
}

func TestRetainedMemoryExceedsLimitConservativelyHandlesZero(t *testing.T) {
	if !retainedMemoryExceedsLimit(0, wasmapi.DefaultMaxRetainedMemoryBytes) {
		t.Fatal("zero-sized reading did not request conservative replacement")
	}
}

func TestProcessDefaultRetainedMemoryLimitKeepsMemorylessInstanceWarm(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(func (export "grow") (result i32)
			i32.const 1
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	first := p.inst
	if first == nil {
		t.Fatal("process discarded memoryless warm instance")
	}
	if p.hasMemory {
		t.Fatal("process classified memoryless instance as linear memory")
	}

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if p.inst != first {
		t.Fatal("process replaced memoryless warm instance")
	}
}

func TestProcessDefaultRetainedMemoryLimitRecyclesAmbiguousZeroSize(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(memory 0)
		(func (export "grow") (result i32)
			memory.size
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); !errors.Is(err, process.ErrProcessReplacementRequested) {
		t.Fatalf("Step() error = %v, want %v", err, process.ErrProcessReplacementRequested)
	}
}

func TestProcessDefaultRetainedMemoryLimitKeepsWarmInstanceBelowLimit(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileMemoryGrowthModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	first := p.inst
	if first == nil {
		t.Fatal("process discarded warm instance below default retained memory limit")
	}

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if p.inst != first {
		t.Fatal("process replaced warm instance below default retained memory limit")
	}
}

func TestProcessDefaultRetainedMemoryLimitRequestsRecycle(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileMemoryGrowthModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	callsBeforeReplacement := int(wasmapi.DefaultMaxRetainedMemoryBytes/(2*64*1024)) + 1
	for call := 1; call <= callsBeforeReplacement; call++ {
		err := runGrowthProcess(t, p)
		if call < callsBeforeReplacement {
			if err != nil {
				t.Fatalf("Step() call %d error = %v", call, err)
			}
			continue
		}
		if !errors.Is(err, process.ErrProcessReplacementRequested) {
			t.Fatalf("Step() call %d error = %v, want %v", call, err, process.ErrProcessReplacementRequested)
		}
	}
}

func TestProcessDefaultRetainedMemoryLimitChecksFirstCall(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(memory 1025)
		(func (export "grow") (result i32)
			memory.size
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); !errors.Is(err, process.ErrProcessReplacementRequested) {
		t.Fatalf("Step() error = %v, want %v", err, process.ErrProcessReplacementRequested)
	}
}

func TestProcessExplicitZeroRetainedMemoryLimitDisablesRecycling(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(memory 1025)
		(func (export "grow") (result i32)
			memory.size
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	limits := wasmapi.LimitsConfig{}
	limits.SetMaxRetainedMemoryBytes(0)
	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, limits, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if p.inst == nil {
		t.Fatal("explicit zero retained-memory limit recycled the instance")
	}
}

func TestProcessDefaultRetainedMemoryLimitAmortizesMemoryChecks(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(memory 1)
		(func (export "grow") (result i32)
			i32.const 3
			memory.grow
			drop
			memory.size
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{}, nil)
	defer p.Close()

	const replacementCall = 353
	for call := 1; call <= replacementCall; call++ {
		err := runGrowthProcess(t, p)
		if call < replacementCall {
			if err != nil {
				t.Fatalf("Step() call %d error = %v", call, err)
			}
			continue
		}
		if !errors.Is(err, process.ErrProcessReplacementRequested) {
			t.Fatalf("Step() call %d error = %v, want %v", call, err, process.ErrProcessReplacementRequested)
		}
	}
}

func TestProcessRetainedMemoryLimitHonoursConfiguredCheckInterval(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module
		(memory 1)
		(func (export "grow") (result i32)
			i32.const 3
			memory.grow
			drop
			memory.size
		)
	)`)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
		MaxRetainedMemoryBytes:      wasmapi.DefaultMaxRetainedMemoryBytes,
		RetainedMemoryCheckInterval: 4,
	}, nil)
	defer p.Close()

	const replacementCall = 345
	for call := 1; call <= replacementCall; call++ {
		err := runGrowthProcess(t, p)
		if call < replacementCall {
			if err != nil {
				t.Fatalf("Step() call %d error = %v", call, err)
			}
			continue
		}
		if !errors.Is(err, process.ErrProcessReplacementRequested) {
			t.Fatalf("Step() call %d error = %v, want %v", call, err, process.ErrProcessReplacementRequested)
		}
	}
}

func TestProcessRetainedMemoryLimitKeepsWarmInstanceBelowLimit(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileMemoryGrowthModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
		MaxRetainedMemoryBytes: 1024 * 1024,
	}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	first := p.inst
	if first == nil {
		t.Fatal("process discarded warm instance below retained memory limit")
	}

	if err := runGrowthProcess(t, p); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	if p.inst != first {
		t.Fatal("process replaced warm instance below retained memory limit")
	}
}

func TestProcessRetainedMemoryLimitRequestsRecycleAboveLimit(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileMemoryGrowthModule(ctx, t)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{
		MaxRetainedMemoryBytes: 64 * 1024,
	}, nil)
	defer p.Close()

	if err := runGrowthProcess(t, p); !errors.Is(err, process.ErrProcessReplacementRequested) {
		t.Fatalf("Step() error = %v, want %v", err, process.ErrProcessReplacementRequested)
	}
	if p.inst == nil {
		t.Fatal("process closed warm instance before pool could replace worker")
	}
}
