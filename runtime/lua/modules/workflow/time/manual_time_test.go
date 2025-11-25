package time_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/workflow"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	workflowtime "github.com/wippyai/runtime/runtime/lua/modules/workflow/time"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

// TestManualTimeAdvancement demonstrates that TimeReference is mutable
// and can be manually advanced during workflow execution
func TestManualTimeAdvancement(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start at fixed time
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	timeRef := newMockTimeReference(startTime)

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Workflow that reads time multiple times
	err = vm.Import(`
		function workflow_time_progression()
			local time = require("time")

			-- Read time at start
			local t1 = time.now()
			local unix1 = t1:unix()

			-- Sleep for 10 seconds
			time.sleep(10000)

			-- Read time after sleep (should reflect advanced time)
			local t2 = time.now()
			local unix2 = t2:unix()

			return {
				time1 = unix1,
				time2 = unix2,
				diff = unix2 - unix1
			}
		end
	`, "test", "workflow_time_progression")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	err = workflowapi.WithTimeReference(frameCtx, timeRef)
	require.NoError(t, err)

	upstream := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstream)
	require.NoError(t, err)

	// Run Execute in goroutine since it blocks waiting for command completion
	var result lua.LValue
	var execErr error
	done := make(chan struct{})

	go func() {
		result, execErr = runner.Execute(frameCtx, "workflow_time_progression")
		close(done)
	}()

	// Wait for command to be sent
	var commands []runtime.Command
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for sleep command")
		case <-done:
			t.Fatal("workflow completed before command was sent")
		default:
			commands = upstream.GetCommands()
			if len(commands) > 0 {
				goto commandReceived
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

commandReceived:
	// Verify timer command was sent
	require.Len(t, commands, 1)
	timerCmd := commands[0]
	assert.Equal(t, runtime.Type("timer.sleep"), timerCmd.Type())

	t.Logf("Initial workflow time: %s", startTime.Format(time.RFC3339))
	t.Logf("Commands captured: %d", len(commands))

	// MANUALLY ADVANCE TIME by 10 seconds
	t.Log("Manually advancing time by 10 seconds...")
	timeRef.AdvanceTime(10 * time.Second)

	newTime := timeRef.Now()
	t.Logf("New workflow time after advancement: %s", newTime.Format(time.RFC3339))

	// Verify time actually advanced
	assert.Equal(t, startTime.Add(10*time.Second).Unix(), newTime.Unix(),
		"TimeReference should be mutable and reflect advanced time")

	// Complete the timer command with the advanced time
	err = timerCmd.Complete(&runtime.Result{
		Value: payload.New(newTime),
	})
	require.NoError(t, err)

	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		t.Logf("Workflow result: %+v", result)
		// The workflow would see the advanced time on next execution
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}
}

// TestManualTimeControl demonstrates full control over workflow time
func TestManualTimeControl(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start at a specific time
	startTime := time.Date(2025, 3, 15, 9, 0, 0, 0, time.UTC)
	timeRef := newMockTimeReference(startTime)

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Workflow that checks time before and after multiple sleeps
	err = vm.Import(`
		function workflow_with_time_checks()
			local time = require("time")

			local times = {}

			-- Check 1: Initial time
			local t0 = time.now()
			table.insert(times, {
				unix = t0:unix(),
				hour = t0:hour(),
				minute = t0:minute()
			})

			-- Sleep 1 hour
			time.sleep(3600000)

			-- Check 2: After 1 hour
			local t1 = time.now()
			table.insert(times, {
				unix = t1:unix(),
				hour = t1:hour(),
				minute = t1:minute()
			})

			-- Sleep 30 minutes
			time.sleep(1800000)

			-- Check 3: After 1.5 hours total
			local t2 = time.now()
			table.insert(times, {
				unix = t2:unix(),
				hour = t2:hour(),
				minute = t2:minute()
			})

			return times
		end
	`, "test", "workflow_with_time_checks")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	err = workflowapi.WithTimeReference(frameCtx, timeRef)
	require.NoError(t, err)

	upstream := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstream)
	require.NoError(t, err)

	// Run Execute in goroutine since it blocks waiting for command completion
	var result lua.LValue
	var execErr error
	done := make(chan struct{})

	go func() {
		result, execErr = runner.Execute(frameCtx, "workflow_with_time_checks")
		close(done)
	}()

	t.Logf("=== Time Progression Test ===")
	t.Logf("Start time: %s", startTime.Format("15:04:05"))

	// Process first timer command (1 hour)
	t.Log("\n--- Processing Timer 1 (1 hour sleep) ---")

	// Wait for first command
	var cmd1 runtime.Command
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for first sleep command")
		case <-done:
			t.Fatal("workflow completed before first command")
		default:
			commands := upstream.FlushRequests()
			if len(commands) > 0 {
				cmd1 = commands[0]
				goto cmd1Received
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

cmd1Received:
	assert.Equal(t, runtime.Type("timer.sleep"), cmd1.Type())

	// Manually advance time by 1 hour
	timeRef.AdvanceTime(1 * time.Hour)
	time1 := timeRef.Now()
	t.Logf("After 1 hour: %s", time1.Format("15:04:05"))
	assert.Equal(t, 10, time1.Hour(), "time should be 10:00 after 1 hour")

	// Complete first timer
	err = cmd1.Complete(&runtime.Result{Value: payload.New(time1)})
	require.NoError(t, err)

	// Process second timer command (30 minutes)
	t.Log("\n--- Processing Timer 2 (30 min sleep) ---")

	// Wait for second command
	var cmd2 runtime.Command
	var time2 time.Time
	timeout = time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for second sleep command")
		case <-done:
			goto allDone
		default:
			commands := upstream.FlushRequests()
			if len(commands) > 0 {
				cmd2 = commands[0]
				goto cmd2Received
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

cmd2Received:
	assert.Equal(t, runtime.Type("timer.sleep"), cmd2.Type())

	// Manually advance time by 30 minutes
	timeRef.AdvanceTime(30 * time.Minute)
	time2 = timeRef.Now()
	t.Logf("After 1h 30m: %s", time2.Format("15:04:05"))
	assert.Equal(t, 10, time2.Hour(), "time should be 10:30")
	assert.Equal(t, 30, time2.Minute(), "time should be 10:30")

	// Complete second timer
	err = cmd2.Complete(&runtime.Result{Value: payload.New(time2)})
	require.NoError(t, err)

allDone:
	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}

	t.Log("\n=== Summary ===")
	t.Logf("Start:          %s", startTime.Format("15:04:05"))
	t.Logf("After 1h:       %s", time1.Format("15:04:05"))
	t.Logf("After 1h 30m:   %s", time2.Format("15:04:05"))
	t.Logf("Workflow result: %+v", result)
}

// TestTimeReferenceIsolation verifies TimeReference is isolated per workflow instance
func TestTimeReferenceIsolation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create two separate time references for two workflow instances
	time1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	time2 := time.Date(2025, 6, 15, 15, 30, 0, 0, time.UTC)

	ref1 := newMockTimeReference(time1)
	ref2 := newMockTimeReference(time2)

	mod := workflowtime.NewTimeModule()

	// Workflow that just returns current time
	luaCode := `
		function get_current_time()
			local time = require("time")
			local now = time.now()
			return now:unix()
		end
	`

	// Execute with first time reference
	vm1, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm1.Close()

	err = vm1.Import(luaCode, "test", "get_current_time")
	require.NoError(t, err)

	runner1 := engine.NewRunner(vm1,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx1 := ctxapi.NewRootContext()
	frameCtx1, _ := ctxapi.OpenFrameContext(ctx1)
	err = workflowapi.WithTimeReference(frameCtx1, ref1)
	require.NoError(t, err)

	upstream1 := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx1, upstream1)
	require.NoError(t, err)

	result1, err := runner1.Execute(frameCtx1, "get_current_time")
	require.NoError(t, err)

	// Execute with second time reference
	vm2, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm2.Close()

	err = vm2.Import(luaCode, "test", "get_current_time")
	require.NoError(t, err)

	runner2 := engine.NewRunner(vm2,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx2 := ctxapi.NewRootContext()
	frameCtx2, _ := ctxapi.OpenFrameContext(ctx2)
	err = workflowapi.WithTimeReference(frameCtx2, ref2)
	require.NoError(t, err)

	upstream2 := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx2, upstream2)
	require.NoError(t, err)

	result2, err := runner2.Execute(frameCtx2, "get_current_time")
	require.NoError(t, err)

	// Verify each workflow sees its own time
	t.Logf("Workflow 1 time: %s (unix: %v)", time1.Format(time.RFC3339), result1)
	t.Logf("Workflow 2 time: %s (unix: %v)", time2.Format(time.RFC3339), result2)

	assert.EqualValues(t, time1.Unix(), result1, "workflow 1 should see time1")
	assert.EqualValues(t, time2.Unix(), result2, "workflow 2 should see time2")
	assert.NotEqual(t, result1, result2, "each workflow should see different time")

	// Advance only ref1's time
	ref1.AdvanceTime(1 * time.Hour)

	// Verify ref2's time is unchanged
	assert.Equal(t, time1.Add(1*time.Hour).Unix(), ref1.Now().Unix())
	assert.Equal(t, time2.Unix(), ref2.Now().Unix())
}

// TestTimeAdvancementDuringExecution shows advancing time between workflow steps
func TestTimeAdvancementDuringExecution(t *testing.T) {
	logger := zaptest.NewLogger(t)

	startTime := time.Date(2025, 1, 1, 8, 0, 0, 0, time.UTC)
	timeRef := newMockTimeReference(startTime)

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Workflow that checks time at multiple points
	err = vm.Import(`
		function workflow_multi_step()
			local time = require("time")
			local results = {}

			-- Step 1: Check initial time
			local t1 = time.now()
			results.step1 = {
				unix = t1:unix(),
				formatted = t1:format(time.RFC3339)
			}

			-- Step 2: Small sleep
			time.sleep(1000) -- 1 second

			-- Step 3: Check time again
			local t2 = time.now()
			results.step2 = {
				unix = t2:unix(),
				formatted = t2:format(time.RFC3339)
			}

			return results
		end
	`, "test", "workflow_multi_step")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	err = workflowapi.WithTimeReference(frameCtx, timeRef)
	require.NoError(t, err)

	upstream := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstream)
	require.NoError(t, err)

	t.Logf("=== Execution Timeline ===")
	t.Logf("Start time: %s", timeRef.Now().Format("15:04:05"))

	// Run Execute in goroutine since it blocks waiting for command completion
	var result lua.LValue
	var execErr error
	done := make(chan struct{})

	go func() {
		result, execErr = runner.Execute(frameCtx, "workflow_multi_step")
		close(done)
	}()

	// Wait for command to be sent
	var commands []runtime.Command
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for sleep command")
		case <-done:
			t.Fatal("workflow completed before command was sent")
		default:
			commands = upstream.GetCommands()
			if len(commands) > 0 {
				goto commandReceived
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

commandReceived:
	require.Len(t, commands, 1)

	// Before completing command, advance time by 5 minutes (not just 1 second)
	t.Log("Advancing time by 5 minutes (workflow asked for 1 second)")
	timeRef.AdvanceTime(5 * time.Minute)
	t.Logf("New time: %s", timeRef.Now().Format("15:04:05"))

	// Complete command with advanced time
	err = commands[0].Complete(&runtime.Result{
		Value: payload.New(timeRef.Now()),
	})
	require.NoError(t, err)

	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		t.Logf("Workflow result: %+v", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}
}
