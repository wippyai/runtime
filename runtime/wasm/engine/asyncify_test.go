package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/runtime/wasm/host"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// WAT module that imports sleep from wippy:clock
const sleepWAT = `(module
  (import "wippy:clock" "sleep" (func $sleep (param i64)))
  (memory (export "memory") 1)

  (func $test_sleep (export "test_sleep") (param $duration i64)
    local.get $duration
    call $sleep
  )

  (func $canonical_abi_realloc (export "canonical_abi_realloc")
    (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32)
    (result i32)
    i32.const 1024
  )

  (func $canonical_abi_free (export "canonical_abi_free")
    (param $ptr i32) (param $size i32) (param $align i32)
  )
)`

// makeSleepCmd creates a SleepCmd from stack args.
func makeSleepCmd(stack []uint64) clockapi.SleepCmd {
	var duration time.Duration
	if len(stack) > 0 {
		duration = time.Duration(int64(stack[0]))
	}
	return clockapi.SleepCmd{Duration: duration}
}

// TestNativeSchedulerYield tests the native wippy scheduler yields dispatcher.Command directly.
func TestNativeSchedulerYield(t *testing.T) {
	ctx := context.Background()

	// Create wazero runtime
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	// Register clock host module using MakeAsyncHandler
	sleepHandler := host.MakeAsyncHandler(func(stack []uint64) clockapi.SleepCmd {
		return makeSleepCmd(stack)
	})

	_, err := rt.NewHostModuleBuilder("wippy:clock").
		NewFunctionBuilder().
		WithGoModuleFunction(sleepHandler, []api.ValueType{api.ValueTypeI64}, nil).
		Export("sleep").
		Instantiate(ctx)
	require.NoError(t, err)

	// Compile WAT and apply asyncify transform
	wasmBytes, err := wasmrt.CompileWAT(sleepWAT)
	require.NoError(t, err)

	asyncifiedBytes, err := CompileWithAsyncify(wasmBytes, []string{"wippy:clock.sleep"})
	require.NoError(t, err)

	// Compile and instantiate
	compiled, err := rt.CompileModule(ctx, asyncifiedBytes)
	require.NoError(t, err)

	inst, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	require.NoError(t, err)
	defer inst.Close(ctx)

	// Setup wippy's native asyncify and scheduler
	asyncify, err := InitAsyncify(inst)
	require.NoError(t, err)

	scheduler := NewScheduler(asyncify)

	// Setup context with wippy's native scheduler
	ctx = WithAsyncify(ctx, asyncify)
	ctx = WithScheduler(ctx, scheduler)

	// Get the test function
	testSleep := inst.ExportedFunction("test_sleep")
	require.NotNil(t, testSleep)

	// Execute with 10ms sleep duration
	sleepDuration := 10 * time.Millisecond
	err = scheduler.Execute(ctx, testSleep, uint64(sleepDuration))
	require.NoError(t, err)

	// Step - should yield sleep command DIRECTLY (no PendingOp wrapping)
	result, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, SchedulerContinue, result.Status, "should yield with SchedulerContinue")
	require.NotNil(t, result.Command, "should have dispatcher.Command directly")

	// Verify it's a SleepCmd with correct duration - NO TYPE ASSERTION NEEDED!
	sleepCmd, ok := result.Command.(clockapi.SleepCmd)
	require.True(t, ok, "command should be SleepCmd directly, got %T", result.Command)
	require.Equal(t, sleepDuration, sleepCmd.Duration, "sleep duration should match")

	// Resume after sleep
	result, err = scheduler.Step(ctx, &YieldResult{})
	require.NoError(t, err)
	require.Equal(t, SchedulerDone, result.Status, "should complete after resume")
}

// TestProcessYieldsDispatcherCommand tests Process.Step() yields dispatcher.Command directly.
func TestProcessYieldsDispatcherCommand(t *testing.T) {
	ctx := context.Background()

	// Use wazero directly for this test to control host registration
	wazeroRt := wazero.NewRuntime(ctx)
	defer wazeroRt.Close(ctx)

	// Register clock host with async handler - correct signature
	sleepHandler := host.MakeAsyncHandler(func(stack []uint64) clockapi.SleepCmd {
		return makeSleepCmd(stack)
	})
	_, err := wazeroRt.NewHostModuleBuilder("wippy:clock").
		NewFunctionBuilder().
		WithGoModuleFunction(sleepHandler, []api.ValueType{api.ValueTypeI64}, nil).
		Export("sleep").
		Instantiate(ctx)
	require.NoError(t, err)

	// Compile WAT and apply asyncify transform
	wasmBytes, err := wasmrt.CompileWAT(sleepWAT)
	require.NoError(t, err)

	asyncifiedBytes, err := CompileWithAsyncify(wasmBytes, []string{"wippy:clock.sleep"})
	require.NoError(t, err)

	// Compile and instantiate
	compiled, err := wazeroRt.CompileModule(ctx, asyncifiedBytes)
	require.NoError(t, err)

	inst, err := wazeroRt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	require.NoError(t, err)
	defer inst.Close(ctx)

	// Setup wippy's native asyncify and scheduler
	asyncify, err := InitAsyncify(inst)
	require.NoError(t, err)

	scheduler := NewScheduler(asyncify)

	// Setup context
	ctx = WithAsyncify(ctx, asyncify)
	ctx = WithScheduler(ctx, scheduler)

	// Get the test function and execute
	testSleep := inst.ExportedFunction("test_sleep")
	require.NotNil(t, testSleep)

	sleepDuration := 5 * time.Millisecond
	err = scheduler.Execute(ctx, testSleep, uint64(sleepDuration))
	require.NoError(t, err)

	// Step should yield dispatcher.Command directly
	result, err := scheduler.Step(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, SchedulerContinue, result.Status)

	// Direct type assertion - no intermediate interface needed!
	sleepCmd, ok := result.Command.(clockapi.SleepCmd)
	require.True(t, ok, "should be SleepCmd directly, got %T", result.Command)
	require.Equal(t, sleepDuration, sleepCmd.Duration)
	t.Logf("Yielded SleepCmd with duration: %v", sleepCmd.Duration)
}

// TestCompileWATWithAsyncify tests the CompileWATWithOptions automatic asyncify.
func TestCompileWATWithAsyncify(t *testing.T) {
	ctx := context.Background()

	rt, err := wasmrt.New(ctx)
	require.NoError(t, err)
	defer rt.Close(ctx)

	// Compile with automatic asyncify transform
	module, err := CompileWATWithOptions(ctx, rt, sleepWAT, "", CompileOptions{
		AsyncImports: []string{"wippy:clock.sleep"},
	})
	require.NoError(t, err)
	require.NotNil(t, module)
}

// BenchmarkSchedulerStep benchmarks the scheduler step without allocations.
func BenchmarkSchedulerStep(b *testing.B) {
	ctx := context.Background()

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)

	sleepHandler := host.MakeAsyncHandler(func(stack []uint64) clockapi.SleepCmd {
		return makeSleepCmd(stack)
	})

	_, err := rt.NewHostModuleBuilder("wippy:clock").
		NewFunctionBuilder().
		WithGoModuleFunction(sleepHandler, []api.ValueType{api.ValueTypeI64}, nil).
		Export("sleep").
		Instantiate(ctx)
	if err != nil {
		b.Fatal(err)
	}

	wasmBytes, _ := wasmrt.CompileWAT(sleepWAT)
	asyncifiedBytes, _ := CompileWithAsyncify(wasmBytes, []string{"wippy:clock.sleep"})
	compiled, _ := rt.CompileModule(ctx, asyncifiedBytes)
	inst, _ := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	defer inst.Close(ctx)

	asyncify, _ := InitAsyncify(inst)
	scheduler := NewScheduler(asyncify)

	ctx = WithAsyncify(ctx, asyncify)
	ctx = WithScheduler(ctx, scheduler)

	testSleep := inst.ExportedFunction("test_sleep")
	sleepDuration := uint64(time.Millisecond)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		scheduler.Reset()
		_ = scheduler.Execute(ctx, testSleep, sleepDuration)
		result, _ := scheduler.Step(ctx, nil)
		if result.Status == SchedulerContinue {
			_, _ = scheduler.Step(ctx, &YieldResult{})
		}
	}
}
