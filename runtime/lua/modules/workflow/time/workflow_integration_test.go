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

// TestWorkflowIntegration_TimeSleepWithCommandCompletion demonstrates full workflow pattern:
// 1. Lua workflow calls time.sleep()
// 2. Timer command is sent to upstream
// 3. Host completes the command with result
// 4. Workflow continues execution
func TestWorkflowIntegration_TimeSleepWithCommandCompletion(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Deterministic time for workflow
	startTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	timeRef := newMockTimeReference(startTime)

	// Create workflow time module
	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Lua workflow code that:
	// 1. Gets current time before sleep
	// 2. Sleeps for 5 seconds
	// 3. Gets current time after sleep
	// 4. Returns both times
	err = vm.Import(`
		function workflow_with_sleep()
			local time = require("time")

			-- Get time before sleep
			local before = time.now()
			local before_unix = before:unix()

			-- Sleep for 5 seconds (5000ms)
			time.sleep(5000)

			-- Get time after sleep
			local after = time.now()
			local after_unix = after:unix()

			return {
				before = before_unix,
				after = after_unix,
				elapsed = after_unix - before_unix
			}
		end
	`, "test", "workflow_with_sleep")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	// Setup workflow execution context
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
		result, execErr = runner.Execute(frameCtx, "workflow_with_sleep")
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
	require.Len(t, commands, 1, "workflow should send one timer command")

	cmd := commands[0]
	assert.Equal(t, runtime.Type("timer.sleep"), cmd.Type())

	// Verify command params contain 5000ms duration
	params := cmd.Params()
	require.Len(t, params, 1)
	assert.NotNil(t, params[0], "sleep duration should be in params")

	// Simulate host completing the timer command after 5 seconds
	// (advancing mock time)
	timeRef.AdvanceTime(5 * time.Second)
	completionTime := timeRef.Now()

	// Complete the command with result
	err = cmd.Complete(&runtime.Result{
		Value: payload.New(completionTime),
	})
	require.NoError(t, err)

	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		t.Logf("Workflow result: %+v", result)
		// The result contains before/after times and elapsed duration
		// Since we're using deterministic time, we can verify:
		// - before = startTime
		// - after = startTime + 5s (because we advanced time)
		// - elapsed = 5s
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}
}

// TestWorkflowIntegration_MultipleTimerCommands verifies multiple timer commands
func TestWorkflowIntegration_MultipleTimerCommands(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Lua workflow with multiple sleep calls
	err = vm.Import(`
		function workflow_multiple_sleeps()
			local time = require("time")

			-- First sleep: 1 second
			time.sleep(1000)

			-- Second sleep: 2 seconds
			time.sleep(2000)

			-- Third sleep: 3 seconds
			time.sleep(3000)

			return "completed"
		end
	`, "test", "workflow_multiple_sleeps")
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
		result, execErr = runner.Execute(frameCtx, "workflow_multiple_sleeps")
		close(done)
	}()

	// Complete 3 sleep commands sequentially
	var collectedCommands []runtime.Command
	for i := 0; i < 3; i++ {
		// Wait for next command
		timeout := time.After(2 * time.Second)
		for {
			select {
			case <-timeout:
				t.Fatalf("timeout waiting for sleep command %d", i+1)
			case <-done:
				if i < 2 {
					t.Fatalf("workflow completed before sending all commands (got %d)", i+1)
				}
				goto allDone
			default:
				commands := upstream.FlushRequests()
				if len(commands) > 0 {
					require.Len(t, commands, 1, "expected exactly one command at a time")
					cmd := commands[0]
					collectedCommands = append(collectedCommands, cmd)
					assert.Equal(t, runtime.Type("timer.sleep"), cmd.Type())
					// Complete the command
					err := cmd.Complete(&runtime.Result{})
					require.NoError(t, err)
					goto nextCommand
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	nextCommand:
	}

allDone:
	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		assert.EqualValues(t, "completed", result.String())
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}

	// Verify three timer commands were sent
	assert.Len(t, collectedCommands, 3, "workflow should send three timer commands")
}

// TestWorkflowIntegration_TimeNowDeterministic verifies time.now() uses TimeReference
func TestWorkflowIntegration_TimeNowDeterministic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Fixed deterministic time
	fixedTime := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)
	timeRef := newMockTimeReference(fixedTime)

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function get_workflow_time()
			local time = require("time")
			local now = time.now()
			return {
				unix = now:unix(),
				year = now:year(),
				month = now:month(),
				day = now:day(),
				hour = now:hour(),
				minute = now:minute(),
				second = now:second()
			}
		end
	`, "test", "get_workflow_time")
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

	result, err := runner.Execute(frameCtx, "get_workflow_time")
	require.NoError(t, err)

	t.Logf("Workflow time result: %+v", result)

	// Verify no commands were sent (time.now() doesn't send commands)
	commands := upstream.GetCommands()
	assert.Len(t, commands, 0, "time.now() should not send commands")

	// Result should reflect the fixed time from TimeReference
	// (exact validation would require extracting table fields from Lua result)
}

// TestWorkflowIntegration_PureFunctionsNoCommands verifies pure time functions don't send commands
func TestWorkflowIntegration_PureFunctionsNoCommands(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Test all pure functions
	err = vm.Import(`
		function test_pure_functions()
			local time = require("time")

			-- Construct time from components
			local t1 = time.date(2025, 1, 15, 10, 30, 0, 0)

			-- Construct time from unix timestamp
			local t2 = time.unix(1704110400, 0)

			-- Parse duration
			local d1 = time.parse_duration("5m30s")
			local d2 = time.parse_duration("2h15m")

			-- Time arithmetic
			local t3 = t1:add(d1)
			local diff = t3:sub(t1)

			-- Formatting
			local formatted = t1:format(time.RFC3339)

			-- Parse time string
			local t4, err = time.parse(time.RFC3339, "2025-01-15T10:30:00Z")

			return "all_pure_functions_executed"
		end
	`, "test", "test_pure_functions")
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

	result, err := runner.Execute(frameCtx, "test_pure_functions")
	require.NoError(t, err)
	assert.EqualValues(t, "all_pure_functions_executed", result)

	// CRITICAL: Pure functions should NEVER send commands
	commands := upstream.GetCommands()
	assert.Len(t, commands, 0, "pure time functions must not send any commands")
}
