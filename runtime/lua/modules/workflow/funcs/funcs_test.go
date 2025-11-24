package funcs_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	workflowfuncs "github.com/wippyai/runtime/runtime/lua/modules/workflow/funcs"
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

func TestWorkflowFuncs_New(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowfuncs.NewFuncsModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_new()
			local funcs = require("funcs")
			local executor = funcs.new()
			return executor ~= nil
		end
	`, "test", "test_new")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	upstreamH := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstreamH)
	require.NoError(t, err)

	result, err := runner.Execute(frameCtx, "test_new")
	require.NoError(t, err)
	assert.Equal(t, lua.LTrue, result)
}

func TestWorkflowFuncs_Call(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowfuncs.NewFuncsModule()
	upstreamMod := upstream.NewUpstreamModule()
	vm, err := engine.NewCVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithLoader(upstreamMod.Name(), upstreamMod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_call()
			local funcs = require("funcs")
			local executor = funcs.new()
			local result, err = executor:call("app.funcs:calculate", 10, 20)
			if err then
				return "error: " .. tostring(err)
			end
			return result or "no_result"
		end
	`, "test", "test_call")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	upstreamH := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstreamH)
	require.NoError(t, err)

	// Run Execute in goroutine since it blocks
	var result lua.LValue
	var execErr error
	done := make(chan struct{})

	go func() {
		result, execErr = runner.Execute(frameCtx, "test_call")
		close(done)
	}()

	// Wait for command
	var commands []runtime.Command
	timeout := time.After(2 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for funcs.call command")
		case <-done:
			t.Fatal("workflow completed before command was sent")
		default:
			commands = upstreamH.GetCommands()
			if len(commands) > 0 {
				goto commandReceived
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

commandReceived:
	require.Len(t, commands, 1)
	cmd := commands[0]
	assert.Equal(t, runtime.Type("funcs.call"), cmd.Type())

	// Complete with result
	err = cmd.Complete(&runtime.Result{
		Value: payload.New(30), // 10 + 20
	})
	require.NoError(t, err)

	// Wait for workflow to complete
	select {
	case <-done:
		require.NoError(t, execErr)
		// Result should be the payload value
		t.Logf("Result: %+v", result)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for workflow completion")
	}
}

func TestWorkflowFuncs_Async(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowfuncs.NewFuncsModule()
	upstreamMod := upstream.NewUpstreamModule()
	vm, err := engine.NewCVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithLoader(upstreamMod.Name(), upstreamMod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	// async returns immediately with a request object
	err = vm.Import(`
		function test_async()
			local funcs = require("funcs")
			local executor = funcs.new()
			local request = executor:async("app.funcs:background_task", "data")
			return request ~= nil
		end
	`, "test", "test_async")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	upstreamH := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstreamH)
	require.NoError(t, err)

	// async should return immediately without blocking
	result, err := runner.Execute(frameCtx, "test_async")
	require.NoError(t, err)
	assert.Equal(t, lua.LTrue, result)

	// Verify command was sent
	commands := upstreamH.GetCommands()
	require.Len(t, commands, 1)
	assert.Equal(t, runtime.Type("funcs.call"), commands[0].Type())
}

func TestWorkflowFuncs_CallWithInvalidID(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mod := workflowfuncs.NewFuncsModule()
	vm, err := engine.NewCVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)
	defer vm.Close()

	err = vm.Import(`
		function test_invalid_id()
			local funcs = require("funcs")
			local executor = funcs.new()
			local result, err = executor:call("invalid_no_namespace")
			return err ~= nil
		end
	`, "test", "test_invalid_id")
	require.NoError(t, err)

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	ctx := ctxapi.NewRootContext()
	frameCtx, _ := ctxapi.OpenFrameContext(ctx)

	upstreamH := &upstreamHandler{}
	err = runtime.WithUpstream(frameCtx, upstreamH)
	require.NoError(t, err)

	result, err := runner.Execute(frameCtx, "test_invalid_id")
	require.NoError(t, err)
	assert.Equal(t, lua.LTrue, result) // should return error

	// No command should be sent for invalid ID
	commands := upstreamH.GetCommands()
	assert.Len(t, commands, 0)
}
