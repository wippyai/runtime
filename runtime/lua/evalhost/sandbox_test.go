package evalhost

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process2"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"go.uber.org/zap"
)

// TestSandbox_ManualStepping demonstrates how to manually step through a Lua process
// and see the yields from the Lua perspective.
func TestSandbox_ManualStepping(t *testing.T) {
	modules := []lua2api.Module{
		json.Module,
		timemod.Module,
	}

	log := zap.NewNop()
	host := NewHost(log, modules, nil)

	// Compile a simple program that sleeps
	program, err := host.Compile(context.Background(), CompileCmd{
		Source: `
			local time = require("time")

			local function handle()
				time.sleep(50 * time.MILLISECOND)
				return "done"
			end

			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"time"},
	})
	require.NoError(t, err)

	// Create process from program
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc, err := host.CreateProcess(ctx, program)
	require.NoError(t, err)
	defer proc.Close()

	// Start execution with input
	err = proc.Execute(ctx, "handle", nil)
	require.NoError(t, err)

	// Step 1: Process runs until it yields (sleep command)
	step1, err := proc.Step(nil)
	require.NoError(t, err)

	t.Logf("Step 1 status: %v", step1.Status)
	t.Logf("Step 1 yields: %d", step1.YieldCount())

	// Should be continuing with a yield
	assert.Equal(t, process2.StepContinue, step1.Status)
	assert.Equal(t, 1, step1.YieldCount())

	// Get the yielded command
	yields := step1.GetYields()
	require.Len(t, yields, 1)

	cmd := yields[0]
	t.Logf("Yield command ID: %d", cmd.CmdID())

	// Should be a sleep command
	assert.Equal(t, clockapi.CmdSleep, cmd.CmdID())

	sleepCmd, ok := cmd.(clockapi.SleepCmd)
	require.True(t, ok)
	t.Logf("Sleep duration: %v", sleepCmd.Duration)
	assert.Equal(t, 50*time.Millisecond, sleepCmd.Duration)

	// Simulate time passing and provide result
	results := process2.AcquireYieldResults()
	results.Data = []any{time.Now().UnixNano()} // Return wakeup time as result
	defer process2.ReleaseYieldResults(results)

	// Step 2: Resume with sleep result
	step2, err := proc.Step(results)
	require.NoError(t, err)

	t.Logf("Step 2 status: %v", step2.Status)
	t.Logf("Step 2 yields: %d", step2.YieldCount())

	// Should be done now
	assert.Equal(t, process2.StepDone, step2.Status)
}

// TestSandbox_MultipleSleeps tests stepping through multiple sleep calls
func TestSandbox_MultipleSleeps(t *testing.T) {
	modules := []lua2api.Module{
		json.Module,
		timemod.Module,
	}

	log := zap.NewNop()
	host := NewHost(log, modules, nil)

	program, err := host.Compile(context.Background(), CompileCmd{
		Source: `
			local time = require("time")

			local function handle()
				time.sleep(10 * time.MILLISECOND)
				time.sleep(20 * time.MILLISECOND)
				time.sleep(30 * time.MILLISECOND)
				return "done"
			end

			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"time"},
	})
	require.NoError(t, err)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc, err := host.CreateProcess(ctx, program)
	require.NoError(t, err)
	defer proc.Close()

	err = proc.Execute(ctx, "handle", nil)
	require.NoError(t, err)

	expectedDurations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}

	var results *process2.YieldResults
	for i, expected := range expectedDurations {
		step, err := proc.Step(results)
		if results != nil {
			process2.ReleaseYieldResults(results)
			results = nil
		}
		require.NoError(t, err)

		t.Logf("Step %d: status=%v, yields=%d", i+1, step.Status, step.YieldCount())

		if step.Status == process2.StepDone {
			t.Logf("Process completed at step %d", i+1)
			break
		}

		assert.Equal(t, process2.StepContinue, step.Status)
		assert.Equal(t, 1, step.YieldCount())

		yields := step.GetYields()
		sleepCmd := yields[0].(clockapi.SleepCmd)
		t.Logf("Sleep %d duration: %v", i+1, sleepCmd.Duration)
		assert.Equal(t, expected, sleepCmd.Duration)

		// Prepare result for next iteration
		results = process2.AcquireYieldResults()
		results.Data = []any{time.Now().UnixNano()}
	}

	// Final step to complete
	if results != nil {
		step, err := proc.Step(results)
		process2.ReleaseYieldResults(results)
		require.NoError(t, err)
		assert.Equal(t, process2.StepDone, step.Status)
	}
}

// TestSandbox_ErrorHandling tests error handling during stepping
func TestSandbox_ErrorHandling(t *testing.T) {
	modules := []lua2api.Module{
		json.Module,
		timemod.Module,
	}

	log := zap.NewNop()
	host := NewHost(log, modules, nil)

	program, err := host.Compile(context.Background(), CompileCmd{
		Source: `
			local function handle()
				error("intentional error")
			end

			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"json"},
	})
	require.NoError(t, err)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc, err := host.CreateProcess(ctx, program)
	require.NoError(t, err)
	defer proc.Close()

	err = proc.Execute(ctx, "handle", nil)
	require.NoError(t, err)

	step, err := proc.Step(nil)

	// Error should be returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "intentional error")
	assert.Equal(t, process2.StepDone, step.Status)
}

// TestSandbox_NoYields tests process that completes without yields
func TestSandbox_NoYields(t *testing.T) {
	modules := []lua2api.Module{
		json.Module,
		timemod.Module,
	}

	log := zap.NewNop()
	host := NewHost(log, modules, nil)

	program, err := host.Compile(context.Background(), CompileCmd{
		Source: `
			local json = require("json")

			local function handle()
				return json.encode({sum = 5 + 3})
			end

			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"json"},
	})
	require.NoError(t, err)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc, err := host.CreateProcess(ctx, program)
	require.NoError(t, err)
	defer proc.Close()

	err = proc.Execute(ctx, "handle", nil)
	require.NoError(t, err)

	step, err := proc.Step(nil)
	require.NoError(t, err)

	// Should complete immediately with no yields
	assert.Equal(t, process2.StepDone, step.Status)
	assert.Equal(t, 0, step.YieldCount())
}

// TestSandbox_TimerYields tests timer operations yield correctly
func TestSandbox_TimerYields(t *testing.T) {
	modules := []lua2api.Module{
		json.Module,
		timemod.Module,
	}

	log := zap.NewNop()
	host := NewHost(log, modules, nil)

	program, err := host.Compile(context.Background(), CompileCmd{
		Source: `
			local time = require("time")

			local function handle()
				local timer = time.timer(100 * time.MILLISECOND)
				local ch = timer:channel()
				local fireTime = ch:receive()
				return fireTime > 0
			end

			return { handle = handle }
		`,
		Method:  "handle",
		Modules: []string{"time"},
	})
	require.NoError(t, err)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	proc, err := host.CreateProcess(ctx, program)
	require.NoError(t, err)
	defer proc.Close()

	err = proc.Execute(ctx, "handle", nil)
	require.NoError(t, err)

	// First step should yield timer start
	step1, err := proc.Step(nil)
	require.NoError(t, err)

	t.Logf("Step 1: status=%v, yields=%d", step1.Status, step1.YieldCount())

	if step1.YieldCount() > 0 {
		yields := step1.GetYields()
		for i, y := range yields {
			t.Logf("  Yield %d: cmd=%d", i, y.CmdID())
		}
	}
}
