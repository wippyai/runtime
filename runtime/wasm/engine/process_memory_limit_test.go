// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
)

const wasmPageBytes = 64 * 1024

// growByTwoPagesModule grows a one-page memory by two pages and returns the
// memory.grow result: the previous page count on success, -1 on denial.
const growByTwoPagesModule = `(module
	(memory 1)
	(func (export "grow") (result i32)
		i32.const 2
		memory.grow
	)
)`

func runMemoryLimitProcess(t *testing.T, limits wasmapi.LimitsConfig, source string) (any, error) {
	t.Helper()
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, source)
	defer func() { _ = rt.Close(ctx) }()

	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, limits, nil)
	defer p.Close()
	require.NoError(t, p.Init(ctxapi.NewRootContext(), "grow", nil))

	var out process.StepOutput
	if err := p.Step(nil, &out); err != nil {
		return nil, err
	}
	require.True(t, out.IsDone())
	return out.Result().Data(), nil
}

func TestProcessMemoryLimitAdmitsGrowthWithinLimit(t *testing.T) {
	result, err := runMemoryLimitProcess(t, wasmapi.LimitsConfig{MemoryBytes: 3 * wasmPageBytes}, growByTwoPagesModule)
	require.NoError(t, err)
	require.Equal(t, "1", fmt.Sprint(result))
}

func TestProcessMemoryLimitDeniesGrowthBeyondLimit(t *testing.T) {
	result, err := runMemoryLimitProcess(t, wasmapi.LimitsConfig{MemoryBytes: 2 * wasmPageBytes}, growByTwoPagesModule)
	require.NoError(t, err)
	require.Equal(t, "-1", fmt.Sprint(result))
}

func TestProcessMemoryLimitRejectsInitialMemoryBeyondLimit(t *testing.T) {
	_, err := runMemoryLimitProcess(t, wasmapi.LimitsConfig{MemoryBytes: 2 * wasmPageBytes}, `(module
		(memory 3)
		(func (export "grow") (result i32)
			memory.size
		)
	)`)
	require.Error(t, err)
}

func TestProcessWithoutMemoryLimitAdmitsGrowth(t *testing.T) {
	result, err := runMemoryLimitProcess(t, wasmapi.LimitsConfig{}, growByTwoPagesModule)
	require.NoError(t, err)
	require.Equal(t, "1", fmt.Sprint(result))
}

func TestProcessMemoryLimitRetainsAndReleasesBudget(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, `(module (memory 1) (func (export "grow") (result i32) i32.const 1 memory.grow))`)
	defer func() { _ = rt.Close(ctx) }()
	p := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, wasmapi.LimitsConfig{MemoryBytes: 2 * wasmPageBytes}, nil)
	defer p.Close()
	for call, want := range []string{"1", "-1"} {
		require.NoError(t, p.Init(ctxapi.NewRootContext(), "grow", nil))
		var output process.StepOutput
		require.NoError(t, p.Step(nil, &output))
		require.True(t, output.IsDone())
		require.Equal(t, want, fmt.Sprint(output.Result().Data()), "call %d", call)
		require.Equal(t, uint64(2*wasmPageBytes), p.MemoryBudget().Usage().Used)
	}
	p.Close()
	require.Zero(t, p.MemoryBudget().Usage().Used, "closed instance retains a memory charge")
}

func TestProcessMemoryLimitsAreIndependentPerInstance(t *testing.T) {
	ctx := context.Background()
	rt, mod := compileTestModule(ctx, t, growByTwoPagesModule)
	defer func() { _ = rt.Close(ctx) }()
	limits := wasmapi.LimitsConfig{MemoryBytes: 3 * wasmPageBytes}
	first := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, limits, nil)
	second := NewProcess(mod, wasmapi.TransportTypePayload, wasmapi.WASIConfig{}, limits, nil)
	defer first.Close()
	defer second.Close()
	for _, p := range []*Process{first, second} {
		require.NoError(t, p.Init(ctxapi.NewRootContext(), "grow", nil))
		var output process.StepOutput
		require.NoError(t, p.Step(nil, &output))
		require.Equal(t, "1", fmt.Sprint(output.Result().Data()))
		require.Equal(t, uint64(3*wasmPageBytes), p.MemoryBudget().Usage().Used)
	}
	first.Close()
	require.Zero(t, first.MemoryBudget().Usage().Used)
	require.Equal(t, uint64(3*wasmPageBytes), second.MemoryBudget().Usage().Used, "closing one instance changed the other's budget")
}
