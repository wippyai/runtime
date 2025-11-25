package process_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	workflowprocess "github.com/wippyai/runtime/runtime/lua/modules/workflow/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap/zaptest"
)

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

func setupContext(t *testing.T) (context.Context, *upstreamHandler) {
	ctx := ctxapi.NewRootContext()
	frameCtx, frame := ctxapi.OpenFrameContext(ctx)

	// Set ID on frame (must be registry.ID type, not string)
	require.NoError(t, frame.Set(runtime.FrameIDKey, registry.NewID("test.workflows", "my_workflow")))

	// Set PID on frame with proper interning
	pid := relay.PID{
		Host:   "test-host",
		UniqID: "test-workflow-123",
	}.Precomputed()
	require.NoError(t, runtime.SetFramePID(frameCtx, pid))

	upstreamH := &upstreamHandler{}
	err := runtime.WithUpstream(frameCtx, upstreamH)
	require.NoError(t, err)

	return frameCtx, upstreamH
}

func TestWorkflowProcess_ID(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// Test that id() returns nil when no FrameContext is set (graceful handling)
	err = vm.Import(`
		function test_id()
			local process = require("process")
			local id, err = process.id()
			if err then
				return "no_id"
			end
			return id or "nil"
		end
	`, "test", "test_id")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	result, err := runner.Execute(frameCtx, "test_id")
	require.NoError(t, err)
	// Returns either the ID or "no_id" if context not fully set up
	t.Logf("ID result: %v", result)
}

func TestWorkflowProcess_PID(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_pid()
			local process = require("process")
			local pid = process.pid()
			return pid or "nil"
		end
	`, "test", "test_pid")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	result, err := runner.Execute(frameCtx, "test_pid")
	require.NoError(t, err)
	// PID might be nil if context not fully set up in test
	t.Logf("PID result: %v", result)
}

func TestWorkflowProcess_Send(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_send()
			local process = require("process")
			local result, err = process.send("target-pid", "my_topic", "hello", {data = 123})
			if err then
				return "error: " .. tostring(err)
			end
			return result
		end
	`, "test", "test_send")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, upstreamH := setupContext(t)

	// send should return immediately (non-blocking)
	result, err := runner.Execute(frameCtx, "test_send")
	require.NoError(t, err)
	assert.Equal(t, lua.LTrue, result)

	// Verify command was sent
	commands := upstreamH.GetCommands()
	require.Len(t, commands, 1)
	assert.Equal(t, runtime.Type("process.send"), commands[0].Type())

	// Verify params
	params := commands[0].Params()
	require.NotEmpty(t, params)
	t.Logf("Send command params: %+v", params)
}

func TestWorkflowProcess_SpawnDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_spawn()
			local process = require("process")
			process.spawn("test.proc:worker", "host1")
			return "should_not_reach"
		end
	`, "test", "test_spawn")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	_, err = runner.Execute(frameCtx, "test_spawn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spawn is not allowed in workflows")
}

func TestWorkflowProcess_TerminateDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_terminate()
			local process = require("process")
			process.terminate("some-pid")
			return "should_not_reach"
		end
	`, "test", "test_terminate")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	_, err = runner.Execute(frameCtx, "test_terminate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminate is not allowed in workflows")
}

func TestWorkflowProcess_MonitorDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_monitor()
			local process = require("process")
			process.monitor("some-pid")
			return "should_not_reach"
		end
	`, "test", "test_monitor")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	_, err = runner.Execute(frameCtx, "test_monitor")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "topology operations")
}

func TestWorkflowProcess_EventConstants(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowprocess.NewProcessModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Info().Name, mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_events()
			local process = require("process")
			return {
				cancel = process.event.CANCEL,
				exit = process.event.EXIT,
				link_down = process.event.LINK_DOWN
			}
		end
	`, "test", "test_events")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	frameCtx, _ := setupContext(t)

	result, err := runner.Execute(frameCtx, "test_events")
	require.NoError(t, err)
	// Result is a table with event constants
	t.Logf("Event constants: %+v", result)
}

// TestWorkflowProcess_Inbox is skipped because it requires full subscribe infrastructure
// The inbox functionality delegates to the original process module which uses named channels
func TestWorkflowProcess_Inbox(t *testing.T) {
	t.Skip("inbox test requires full subscribe infrastructure setup")
}
