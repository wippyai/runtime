package wasi_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host"
	"github.com/wippyai/runtime/runtime/wasm/host/wasi/clocks"
	"github.com/wippyai/runtime/runtime/wasm/host/wasi/io"
	"github.com/wippyai/runtime/runtime/wasm/resource"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// WAT module that imports WASI clock and poll functions.
// Simulates the pattern: subscribe-duration() -> block()
const wasiSleepWAT = `(module
  ;; Import WASI monotonic clock
  (import "wasi:clocks/monotonic-clock@0.2.8" "subscribe-duration"
    (func $subscribe_duration (param i64) (result i32)))

  ;; Import WASI poll
  (import "wasi:io/poll@0.2.8" "[method]pollable.block"
    (func $pollable_block (param i32)))
  (import "wasi:io/poll@0.2.8" "[resource-drop]pollable"
    (func $pollable_drop (param i32)))

  (memory (export "memory") 1)

  ;; Sleep function: creates pollable, blocks on it, drops it
  (func $sleep (export "sleep") (param $duration_ns i64)
    (local $pollable i32)
    ;; Create pollable for duration
    (local.set $pollable (call $subscribe_duration (local.get $duration_ns)))
    ;; Block until ready
    (call $pollable_block (local.get $pollable))
    ;; Drop the pollable
    (call $pollable_drop (local.get $pollable))
  )

  ;; Standard CABI exports
  (func $canonical_abi_realloc (export "canonical_abi_realloc")
    (param $old_ptr i32) (param $old_size i32) (param $align i32) (param $new_size i32)
    (result i32)
    i32.const 1024
  )
  (func $canonical_abi_free (export "canonical_abi_free")
    (param $ptr i32) (param $size i32) (param $align i32)
  )
)`

// TestWASIClockWithScheduler tests WASI clock hosts with wippy scheduler.
func TestWASIClockWithScheduler(t *testing.T) {
	ctx := context.Background()

	// Create wazero runtime
	wazeroRt := wazero.NewRuntime(ctx)
	defer wazeroRt.Close(ctx)

	// Create shared resources for WASI
	resources := resource.NewInstanceResources()
	defer resources.Close()

	// Create hosts with shared resources
	clockHost := clocks.NewMonotonicClockHost(resources)
	pollHost := io.NewPollHost(resources)

	// Register WASI clock host - subscribe-duration creates pollable
	_, err := wazeroRt.NewHostModuleBuilder(clockHost.Namespace()).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			duration := time.Duration(stack[0]) //nolint:gosec // wasm duration
			pollable := &resource.Pollable{Ready: duration == 0}
			handle := resources.Pollables().Insert(pollable)
			resources.TimerDurations().Store(handle, duration)
			stack[0] = uint64(handle)
		}), []api.ValueType{api.ValueTypeI64}, []api.ValueType{api.ValueTypeI32}).
		Export("subscribe-duration").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = uint64(time.Since(clocks.MonotonicStart()).Nanoseconds())
		}), nil, []api.ValueType{api.ValueTypeI64}).
		Export("now").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			stack[0] = 1
		}), nil, []api.ValueType{api.ValueTypeI64}).
		Export("resolution").
		Instantiate(ctx)
	require.NoError(t, err, "Failed to register clock host")

	// Register WASI poll host with async block using MakeAsyncHandler
	blockHandler := host.MakeAsyncHandler(func(stack []uint64) clockapi.SleepCmd {
		if len(stack) == 0 {
			return clockapi.SleepCmd{}
		}
		handle := resource.Handle(stack[0])
		duration, ok := resources.TimerDurations().Load(handle)
		if !ok {
			return clockapi.SleepCmd{}
		}
		return clockapi.SleepCmd{Duration: duration}
	})

	_, err = wazeroRt.NewHostModuleBuilder(pollHost.Namespace()).
		NewFunctionBuilder().
		WithGoModuleFunction(blockHandler, []api.ValueType{api.ValueTypeI32}, nil).
		Export("[method]pollable.block").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			handle := resource.Handle(stack[0])
			resources.TimerDurations().Delete(handle)
			resources.Table().Remove(handle)
		}), []api.ValueType{api.ValueTypeI32}, nil).
		Export("[resource-drop]pollable").
		Instantiate(ctx)
	require.NoError(t, err, "Failed to register poll host")

	// Compile WAT to WASM
	wasmBytes, err := wasmrt.CompileWAT(wasiSleepWAT)
	require.NoError(t, err, "Failed to compile WAT")

	// Apply asyncify transform
	asyncifiedBytes, err := engine.CompileWithAsyncify(wasmBytes, []string{
		"wasi:io/poll@0.2.8.[method]pollable.block",
	})
	require.NoError(t, err, "Asyncify transform failed")

	// Compile and instantiate
	compiled, err := wazeroRt.CompileModule(ctx, asyncifiedBytes)
	require.NoError(t, err, "Compile failed")

	inst, err := wazeroRt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	require.NoError(t, err, "Instantiate failed")
	defer inst.Close(ctx)

	// Initialize asyncify and scheduler
	asyncify, err := engine.InitAsyncify(inst)
	require.NoError(t, err, "InitAsyncify failed")

	scheduler := engine.NewScheduler(asyncify)
	ctx = engine.WithAsyncify(ctx, asyncify)
	ctx = engine.WithScheduler(ctx, scheduler)

	// Get the sleep function
	sleepFn := inst.ExportedFunction("sleep")
	require.NotNil(t, sleepFn, "sleep function not found")

	// Execute with 50ms sleep
	sleepDuration := 50 * time.Millisecond
	err = scheduler.Execute(ctx, sleepFn, uint64(sleepDuration))
	require.NoError(t, err)

	start := time.Now()
	var stepCount int

	// Run scheduler loop
	var yieldResult *engine.YieldResult
	for {
		result, err := scheduler.Step(ctx, yieldResult)
		if yieldResult != nil {
			engine.ReleaseYieldResult(yieldResult)
			yieldResult = nil
		}
		require.NoError(t, err)
		stepCount++

		if result.Status == engine.SchedulerDone {
			break
		}

		if result.Status == engine.SchedulerContinue {
			sleepCmd, ok := result.Command.(clockapi.SleepCmd)
			require.True(t, ok, "Expected SleepCmd, got %T", result.Command)
			t.Logf("Yielded SleepCmd with duration: %v", sleepCmd.Duration)

			time.Sleep(sleepCmd.Duration)
			yieldResult = engine.AcquireYieldResult()
		}
	}

	elapsed := time.Since(start)
	t.Logf("Completed in %v after %d steps", elapsed, stepCount)

	require.GreaterOrEqual(t, elapsed, 40*time.Millisecond, "Sleep was too short")
	require.Less(t, elapsed, 200*time.Millisecond, "Sleep was too long")
}

// TestWASIClockMultipleSleeps tests multiple sleep iterations.
func TestWASIClockMultipleSleeps(t *testing.T) {
	ctx := context.Background()

	wazeroRt := wazero.NewRuntime(ctx)
	defer wazeroRt.Close(ctx)

	resources := resource.NewInstanceResources()
	defer resources.Close()

	clockHost := clocks.NewMonotonicClockHost(resources)
	pollHost := io.NewPollHost(resources)

	// Register hosts
	_, err := wazeroRt.NewHostModuleBuilder(clockHost.Namespace()).
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			duration := time.Duration(stack[0]) //nolint:gosec // wasm duration
			pollable := &resource.Pollable{Ready: duration == 0}
			handle := resources.Pollables().Insert(pollable)
			resources.TimerDurations().Store(handle, duration)
			stack[0] = uint64(handle)
		}), []api.ValueType{api.ValueTypeI64}, []api.ValueType{api.ValueTypeI32}).
		Export("subscribe-duration").
		Instantiate(ctx)
	require.NoError(t, err)

	blockHandler := host.MakeAsyncHandler(func(stack []uint64) clockapi.SleepCmd {
		if len(stack) == 0 {
			return clockapi.SleepCmd{}
		}
		handle := resource.Handle(stack[0])
		duration, ok := resources.TimerDurations().Load(handle)
		if !ok {
			return clockapi.SleepCmd{}
		}
		return clockapi.SleepCmd{Duration: duration}
	})

	_, err = wazeroRt.NewHostModuleBuilder(pollHost.Namespace()).
		NewFunctionBuilder().
		WithGoModuleFunction(blockHandler, []api.ValueType{api.ValueTypeI32}, nil).
		Export("[method]pollable.block").
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			handle := resource.Handle(stack[0])
			resources.TimerDurations().Delete(handle)
			resources.Table().Remove(handle)
		}), []api.ValueType{api.ValueTypeI32}, nil).
		Export("[resource-drop]pollable").
		Instantiate(ctx)
	require.NoError(t, err)

	wasmBytes, err := wasmrt.CompileWAT(wasiSleepWAT)
	require.NoError(t, err)

	asyncifiedBytes, err := engine.CompileWithAsyncify(wasmBytes, []string{
		"wasi:io/poll@0.2.8.[method]pollable.block",
	})
	require.NoError(t, err)

	compiled, err := wazeroRt.CompileModule(ctx, asyncifiedBytes)
	require.NoError(t, err)

	inst, err := wazeroRt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	require.NoError(t, err)
	defer inst.Close(ctx)

	asyncify, err := engine.InitAsyncify(inst)
	require.NoError(t, err)

	scheduler := engine.NewScheduler(asyncify)
	ctx = engine.WithAsyncify(ctx, asyncify)
	ctx = engine.WithScheduler(ctx, scheduler)

	sleepFn := inst.ExportedFunction("sleep")
	require.NotNil(t, sleepFn)

	// Run 3 sleeps of 20ms each
	sleepDuration := 20 * time.Millisecond
	start := time.Now()

	for i := 0; i < 3; i++ {
		scheduler.Reset()
		err = scheduler.Execute(ctx, sleepFn, uint64(sleepDuration))
		require.NoError(t, err)

		var yieldResult *engine.YieldResult
		for {
			result, err := scheduler.Step(ctx, yieldResult)
			if yieldResult != nil {
				engine.ReleaseYieldResult(yieldResult)
				yieldResult = nil
			}
			require.NoError(t, err)

			if result.Status == engine.SchedulerDone {
				break
			}

			if result.Status == engine.SchedulerContinue {
				sleepCmd := result.Command.(clockapi.SleepCmd)
				time.Sleep(sleepCmd.Duration)
				yieldResult = engine.AcquireYieldResult()
			}
		}
	}

	elapsed := time.Since(start)
	t.Logf("3 sleeps completed in %v", elapsed)

	require.GreaterOrEqual(t, elapsed, 50*time.Millisecond, "Total sleep was too short")
}
