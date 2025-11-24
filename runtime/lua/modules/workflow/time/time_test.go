package time_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/workflow"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	workflowtime "github.com/wippyai/runtime/runtime/lua/modules/workflow/time"
	"go.uber.org/zap/zaptest"
)

// mockTimeReference implements workflowapi.TimeReference for testing
type mockTimeReference struct {
	currentTime time.Time
	startTime   time.Time
	mu          sync.Mutex
}

func newMockTimeReference(start time.Time) *mockTimeReference {
	return &mockTimeReference{
		currentTime: start,
		startTime:   start,
	}
}

func (m *mockTimeReference) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTime
}

func (m *mockTimeReference) StartTime() time.Time {
	return m.startTime
}

func (m *mockTimeReference) AdvanceTime(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = m.currentTime.Add(d)
}

// upstreamHandler captures commands sent by workflow
type upstreamHandler struct {
	commands []runtime.Command
	mu       sync.Mutex
}

func (h *upstreamHandler) SendRequest(cmd runtime.Command) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.commands = append(h.commands, cmd)
	return nil
}

func (h *upstreamHandler) FlushRequests() []runtime.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]runtime.Command, len(h.commands))
	copy(result, h.commands)
	h.commands = h.commands[:0]
	return result
}

func (h *upstreamHandler) GetCommands() []runtime.Command {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]runtime.Command, len(h.commands))
	copy(result, h.commands)
	return result
}

func TestWorkflowTime_Now(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_now()
			local time = require("time")
			local t = time.now()
			return t:unix()
		end
	`, "test", "test_now")
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

	result, err := runner.Execute(frameCtx, "test_now")
	require.NoError(t, err)

	expectedUnix := timeRef.Now().Unix()
	assert.EqualValues(t, expectedUnix, result)
}

func TestWorkflowTime_Sleep(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_sleep()
			local time = require("time")
			time.sleep(5000) -- 5 seconds in milliseconds
			return "done"
		end
	`, "test", "test_sleep")
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
	var result interface{}
	var execErr error
	done := make(chan struct{})

	go func() {
		result, execErr = runner.Execute(frameCtx, "test_sleep")
		close(done)
	}()

	// Wait for command to be sent (poll with timeout)
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
	// Verify command properties
	require.Len(t, commands, 1)
	cmd := commands[0]
	assert.Equal(t, runtime.Type("timer.sleep"), cmd.Type())

	params := cmd.Params()
	require.Len(t, params, 1)
	assert.NotNil(t, params[0])

	// Complete the command to unblock the workflow
	err = cmd.Complete(&runtime.Result{})
	require.NoError(t, err)

	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		assert.EqualValues(t, "done", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}
}

func TestWorkflowTime_Timer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_timer()
			local time = require("time")
			local timer = time.timer(3000)
			return "timer_created"
		end
	`, "test", "test_timer")
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

	result, err := runner.Execute(frameCtx, "test_timer")
	require.NoError(t, err)
	assert.EqualValues(t, "timer_created", result)

	// Timer creation itself doesn't send command until it's used
	commands := upstream.GetCommands()
	assert.Len(t, commands, 0)
}

func TestWorkflowTime_TickerNotSupported(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_ticker()
			local time = require("time")
			local ticker = time.ticker(1000)
			return "should_not_reach"
		end
	`, "test", "test_ticker")
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

	_, err = runner.Execute(frameCtx, "test_ticker")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "time.ticker() is not supported in workflows")
}

func TestWorkflowTime_PureFunctions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	timeRef := newMockTimeReference(time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC))

	mod := workflowtime.NewTimeModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_pure()
			local time = require("time")

			-- Test date construction
			local t = time.date(2025, 1, 15, 10, 30, 0, 0)

			-- Test unix timestamp
			local ts = time.unix(1704110400, 0)

			-- Test parse_duration
			local d = time.parse_duration("5m30s")

			return "pure_functions_work"
		end
	`, "test", "test_pure")
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

	result, err := runner.Execute(frameCtx, "test_pure")
	require.NoError(t, err)
	assert.EqualValues(t, "pure_functions_work", result)

	// Pure functions should not generate commands
	commands := upstream.GetCommands()
	assert.Len(t, commands, 0)
}
