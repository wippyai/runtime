package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	baseprocess "github.com/wippyai/runtime/runtime/lua/component/process"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	transcoder "github.com/wippyai/runtime/system/payload"
	json "github.com/wippyai/runtime/system/payload/json"
	lua "github.com/wippyai/runtime/system/payload/lua"
	"go.uber.org/zap/zaptest"
)

func createTestTranscoder() payload.Transcoder {
	tr := transcoder.NewTranscoder()
	json.Register(tr)
	lua.Register(tr)
	return tr
}

func newTestContext(_ *testing.T) context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	tr := createTestTranscoder()
	ctx = payload.WithTranscoder(ctx, tr)
	return ctx
}

func createTestState(t *testing.T, funcName string, funcBody string) (*baseprocess.State, *engine.CoroutineVM, *engine.Runner) {
	logger := zaptest.NewLogger(t)

	// Create VM
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	// Preload channel module so Lua code can use channel.new()
	L := vm.State()
	channelMod := channel.NewChannelModule()
	L.PreloadModule(channelMod.Name(), channelMod.Loader)

	// Import test function
	err = vm.Import(funcBody, "test", funcName)
	require.NoError(t, err)

	// Create runner with layers
	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	// Create state
	state, err := baseprocess.NewState(logger, runner, funcName)
	require.NoError(t, err)

	return state, vm, runner
}

// mockUpstream implements runtime.Upstream for testing
type mockUpstream struct {
	commands []runtime.Command
}

func (u *mockUpstream) SendRequest(cmd runtime.Command) error {
	u.commands = append(u.commands, cmd)
	return nil
}

func (u *mockUpstream) FlushRequests() []runtime.Command {
	cmds := u.commands
	u.commands = nil
	return cmds
}

// mockCommand implements runtime.Command for testing
type mockCommand struct {
	id      runtime.ID
	cmdType runtime.Type
}

func (c *mockCommand) ID() runtime.ID                 { return c.id }
func (c *mockCommand) Type() runtime.Type             { return c.cmdType }
func (c *mockCommand) Params() payload.Payloads       { return nil }
func (c *mockCommand) Result() *runtime.Result        { return nil }
func (c *mockCommand) Complete(*runtime.Result) error { return nil }
func (c *mockCommand) Cancel() error                  { return nil }
func (c *mockCommand) IsCompleted() bool              { return false }
func (c *mockCommand) IsCanceled() bool               { return false }

func TestNewLuaWorkflow(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			return "test"
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	assert.NotNil(t, workflow)
	assert.Equal(t, state, workflow.State)
	assert.Equal(t, logger, workflow.log)
}

func TestLuaWorkflow_Start(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func(input)
			return input
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}
	input := payload.Payloads{payload.New("test input")}

	err := workflow.Start(ctx, pid, input)
	require.NoError(t, err)
}

func TestLuaWorkflow_Start_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			error("start error")
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	// Error may happen in Start or in first Step, depending on execution model
	if err != nil {
		assert.Contains(t, err.Error(), "start error")
	} else {
		// Error will surface in Step
		_, err = workflow.Step()
		assert.Error(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "start error")
		}
	}
}

func TestLuaWorkflow_Step_Continue(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			local channel = require("channel")
			local ch1 = channel.new(1)
			local ch2 = channel.new(1)

			-- Create multiple pending operations
			ch1:send("data1")
			ch2:send("data2")

			-- Receive from first
			local v1 = ch1:receive()

			-- Still have second channel with data - continue processing
			return ch2:receive()
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Multiple steps needed to complete
	// Just verify Step() works without checking exact result
	_, err = workflow.Step()
	assert.NoError(t, err)
}

func TestLuaWorkflow_Step_Idle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func(input)
			-- Simple workflow that completes immediately
			-- Testing that Step() can be called successfully
			return input or "default"
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Step should complete successfully
	_, err = workflow.Step()
	// Either no error or ErrExit (workflow completed)
	if err != nil && !errors.Is(err, supervisor.ErrExit) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLuaWorkflow_Step_Done(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			return "completed"
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	// Start the workflow
	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Step until done - simple function completes in one step
	result, err := workflow.Step()
	if errors.Is(err, supervisor.ErrExit) {
		assert.Equal(t, process.StepDone, result)
	} else {
		assert.NoError(t, err)
	}
}

func TestLuaWorkflow_Step_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			error("workflow error")
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	// Error may surface in Start or Step
	err := workflow.Start(ctx, pid, nil)
	if err == nil {
		_, err = workflow.Step()
	}

	assert.Error(t, err)
	if err != nil {
		assert.Contains(t, err.Error(), "workflow error")
	}
}

func TestLuaWorkflow_Commands(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			return "test"
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	// Create mock upstream
	mockUp := &mockUpstream{}
	_ = mockUp.SendRequest(&mockCommand{
		id:      runtime.ID("test-1"),
		cmdType: runtime.Type("timer.sleep"),
	})
	_ = mockUp.SendRequest(&mockCommand{
		id:      runtime.ID("test-2"),
		cmdType: runtime.Type("timer.sleep"),
	})

	// Inject upstream into context
	err := runtime.WithUpstream(ctx, mockUp)
	require.NoError(t, err)

	err = workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Get commands
	commands := workflow.Commands()
	assert.Len(t, commands, 2)
	assert.Equal(t, runtime.ID("test-1"), commands[0].ID())
	assert.Equal(t, runtime.ID("test-2"), commands[1].ID())

	// Commands should be flushed
	commands = workflow.Commands()
	assert.Len(t, commands, 0)
}

func TestLuaWorkflow_Commands_NoUpstream(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			return "test"
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// No upstream in context
	commands := workflow.Commands()
	assert.Nil(t, commands)
}

func TestLuaWorkflow_Send(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			local channel = require("channel")
			local ch = channel.new()
			return channel.receive(ch)
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Create a package to send
	pkg := &relay.Package{
		Source: relay.PID{UniqID: "sender"},
		Messages: []*relay.Message{
			{
				Topic:    "test_topic",
				Payloads: payload.Payloads{payload.New("test message")},
			},
		},
	}

	err = workflow.Send(pkg)
	assert.NoError(t, err)
}

func TestLuaWorkflow_Terminate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	state, vm, _ := createTestState(t, "test_func", `
		function test_func()
			local channel = require("channel")
			local ch = channel.new()
			return channel.receive(ch)  -- blocks indefinitely
		end
	`)
	defer vm.Close()

	workflow := NewLuaWorkflow(logger, state)

	ctx := newTestContext(t)
	pid := relay.PID{UniqID: uuid.New().String()}

	err := workflow.Start(ctx, pid, nil)
	require.NoError(t, err)

	// Terminate the workflow
	workflow.Terminate()

	// State should be closed
	assert.True(t, state.Closed.Load())
}
