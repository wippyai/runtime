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

func compileMemoryGrowthModule(ctx context.Context, t *testing.T) (*wasmrt.Runtime, *wasmrt.Module) {
	t.Helper()

	rt, err := wasmrt.New(ctx)
	if err != nil {
		t.Fatalf("wasm runtime New() error = %v", err)
	}

	mod, err := rt.LoadWAT(ctx, `(module
		(memory 1)
		(func (export "grow") (result i32)
			i32.const 2
			memory.grow
			drop
			memory.size
		)
	)`, "grow: func() -> s32;")
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
