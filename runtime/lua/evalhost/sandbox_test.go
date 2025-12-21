package evalhost

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"go.uber.org/zap"
)

// helper to run step and return yields
func runStep(t *testing.T, proc process.Process, events []process.Event) *process.StepOutput {
	out := &process.StepOutput{}
	err := proc.Step(events, out)
	if err != nil && out.Status() != process.StepDone {
		t.Fatalf("step error: %v", err)
	}
	return out
}

// TestSandbox_ManualStepping demonstrates how to manually step through a Lua process
// and see the yields from the Lua perspective.
func TestSandbox_ManualStepping(t *testing.T) {
	modules := []*luaapi.ModuleDef{
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
	err = proc.Init(ctx, "handle", nil)
	require.NoError(t, err)

	// Step 1: Process runs until it yields (sleep command)
	out1 := runStep(t, proc, nil)

	t.Logf("Step 1 status: %v", out1.Status())
	t.Logf("Step 1 yields: %d", out1.Count())

	// Should be yielding (waiting for sleep completion)
	assert.Equal(t, process.StepYield, out1.Status())
	assert.Equal(t, 1, out1.Count())

	// Get the yielded command
	yields := out1.Yields()
	require.Len(t, yields, 1)

	cmd := yields[0].Cmd
	tag := yields[0].Tag
	t.Logf("Yield command ID: %d", cmd.CmdID())

	// Should be a sleep command
	assert.Equal(t, clockapi.Sleep, cmd.CmdID())

	sleepCmd, ok := cmd.(clockapi.SleepCmd)
	require.True(t, ok)
	t.Logf("Sleep duration: %v", sleepCmd.Duration)
	assert.Equal(t, 50*time.Millisecond, sleepCmd.Duration)

	// Simulate time passing and provide result via event
	events := []process.Event{{
		Type: process.EventYieldComplete,
		Tag:  tag,
		Data: time.Now().UnixNano(),
	}}

	// Step 2: Resume with sleep result
	out2 := runStep(t, proc, events)

	t.Logf("Step 2 status: %v", out2.Status())
	t.Logf("Step 2 yields: %d", out2.Count())

	// Should be done now
	assert.Equal(t, process.StepDone, out2.Status())
}

// TestSandbox_MultipleSleeps tests stepping through multiple sleep calls
func TestSandbox_MultipleSleeps(t *testing.T) {
	modules := []*luaapi.ModuleDef{
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

	err = proc.Init(ctx, "handle", nil)
	require.NoError(t, err)

	expectedDurations := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		30 * time.Millisecond,
	}

	var events []process.Event
	for i, expected := range expectedDurations {
		out := runStep(t, proc, events)
		events = nil

		t.Logf("Step %d: status=%v, yields=%d", i+1, out.Status(), out.Count())

		if out.Status() == process.StepDone {
			t.Logf("Process completed at step %d", i+1)
			break
		}

		assert.Equal(t, process.StepYield, out.Status())
		assert.Equal(t, 1, out.Count())

		yields := out.Yields()
		sleepCmd := yields[0].Cmd.(clockapi.SleepCmd)
		tag := yields[0].Tag
		t.Logf("Sleep %d duration: %v", i+1, sleepCmd.Duration)
		assert.Equal(t, expected, sleepCmd.Duration)

		// Prepare event for next iteration
		events = []process.Event{{
			Type: process.EventYieldComplete,
			Tag:  tag,
			Data: time.Now().UnixNano(),
		}}
	}

	// Final step to complete
	if events != nil {
		out := runStep(t, proc, events)
		assert.Equal(t, process.StepDone, out.Status())
	}
}

// TestSandbox_ErrorHandling tests error handling during stepping
func TestSandbox_ErrorHandling(t *testing.T) {
	modules := []*luaapi.ModuleDef{
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

	err = proc.Init(ctx, "handle", nil)
	require.NoError(t, err)

	out := &process.StepOutput{}
	err = proc.Step(nil, out)

	// Error should be returned
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intentional error")
	assert.Equal(t, process.StepDone, out.Status())
}

// TestSandbox_NoYields tests process that completes without yields
func TestSandbox_NoYields(t *testing.T) {
	modules := []*luaapi.ModuleDef{
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

	err = proc.Init(ctx, "handle", nil)
	require.NoError(t, err)

	out := runStep(t, proc, nil)

	// Should complete immediately with no yields
	assert.Equal(t, process.StepDone, out.Status())
	assert.Equal(t, 0, out.Count())
}

// TestSandbox_TimerYields tests timer operations yield correctly
func TestSandbox_TimerYields(t *testing.T) {
	modules := []*luaapi.ModuleDef{
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

	err = proc.Init(ctx, "handle", nil)
	require.NoError(t, err)

	// First step should yield timer start
	out := runStep(t, proc, nil)

	t.Logf("Step 1: status=%v, yields=%d", out.Status(), out.Count())

	if out.Count() > 0 {
		yields := out.Yields()
		for i, y := range yields {
			t.Logf("  Yield %d: cmd=%d", i, y.Cmd.CmdID())
		}
	}
}
