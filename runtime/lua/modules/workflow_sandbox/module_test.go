package sandbox

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	"go.uber.org/zap/zaptest"
)

// mockCommand implements runtime.Command for testing
type mockCommand struct {
	mu        sync.Mutex
	id        runtime.ID
	cmdType   runtime.Type
	params    payload.Payloads
	completed bool
	result    *runtime.Result
}

func newMockCommand(cmdType string, params ...payload.Payload) *mockCommand {
	return &mockCommand{
		id:      runtime.ID(fmt.Sprintf("mock-%s-%d", cmdType, time.Now().UnixNano())),
		cmdType: runtime.Type(cmdType),
		params:  params,
	}
}

func (c *mockCommand) ID() runtime.ID {
	return c.id
}

func (c *mockCommand) Type() runtime.Type {
	return c.cmdType
}

func (c *mockCommand) Params() payload.Payloads {
	return c.params
}

func (c *mockCommand) Result() *runtime.Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.result
}

func (c *mockCommand) Complete(result *runtime.Result) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.completed {
		return fmt.Errorf("already completed")
	}
	c.completed = true
	c.result = result
	return nil
}

func (c *mockCommand) Cancel() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.completed {
		return fmt.Errorf("already completed")
	}
	c.completed = true
	c.result = &runtime.Result{Error: fmt.Errorf("canceled")}
	return nil
}

func (c *mockCommand) IsCompleted() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.completed
}

func (c *mockCommand) IsCanceled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.completed && c.result != nil && c.result.Error != nil
}

// mockWorkflow implements process.Workflow for testing
type mockWorkflow struct {
	mu           sync.Mutex
	started      bool
	stepCount    int
	input        payload.Payloads
	shouldFail   bool
	maxSteps     int
	completeType string
	ctx          context.Context
	pid          relay.PID
}

func (w *mockWorkflow) Start(ctx context.Context, pid relay.PID, input payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.input = input
	w.ctx = ctx
	w.pid = pid
	return nil
}

func (w *mockWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	if w.shouldFail && w.stepCount == 2 {
		return process.StepContinue, fmt.Errorf("mock error")
	}

	// Create commands for each step until maxSteps
	if w.stepCount < w.maxSteps {
		cmd := newMockCommand("timer.sleep", payload.New(5000))

		// Send command to upstream handler in context (like real workflow does)
		if w.ctx != nil {
			if upstream, ok := runtime.GetUpstream(w.ctx); ok {
				_ = upstream.SendRequest(cmd)
			}
		}

		return process.StepContinue, nil
	}

	// On the final step, complete the workflow and trigger OnComplete
	if w.stepCount >= w.maxSteps {
		cmd := newMockCommand(w.completeType)

		// Send final command to upstream
		if w.ctx != nil {
			if upstream, ok := runtime.GetUpstream(w.ctx); ok {
				_ = upstream.SendRequest(cmd)
			}
		}

		// Trigger OnComplete hook to simulate workflow completion
		w.triggerOnComplete(nil)

		return process.StepDone, nil
	}

	return process.StepContinue, nil
}

func (w *mockWorkflow) triggerOnComplete(err error) {
	if w.ctx == nil {
		return
	}

	// Get OnComplete hooks from context and call them
	hooks := process.GetOnCompleteHooks(w.ctx)
	if hooks != nil {
		result := &runtime.Result{}
		if err != nil {
			result.Error = err
		}
		for _, hook := range hooks {
			hook(w.ctx, w.pid, result)
		}
	}
}

func (w *mockWorkflow) Commands() []runtime.Command {
	// Commands are retrieved from the upstream handler in context, not here
	// This method is here for interface compliance but not used
	return nil
}

func (w *mockWorkflow) Send(_ *relay.Package) error {
	return nil
}

func (w *mockWorkflow) Terminate() {}

// mockPrototypeFactory implements process.PrototypeFactory
type mockPrototypeFactory struct {
	prototypes map[string]func() process.Workflow
}

func (f *mockPrototypeFactory) Create(id registry.ID) (process.Process, error) {
	if proto, ok := f.prototypes[id.String()]; ok {
		return proto(), nil
	}
	return nil, fmt.Errorf("workflow %s not found", id.String())
}

func (f *mockPrototypeFactory) Start(_ context.Context) error {
	return nil
}

func (f *mockPrototypeFactory) Stop() error {
	return nil
}

func setupTestEnvironment(t *testing.T) (*engine.CoroutineVM, *engine.Runner, context.Context, process.PrototypeFactory) {
	logger := zaptest.NewLogger(t)

	// Create mock prototype factory with test workflows
	// Each call to Create should return a NEW instance
	factory := &mockPrototypeFactory{
		prototypes: map[string]func() process.Workflow{
			"app.workflows:simple": func() process.Workflow {
				return &mockWorkflow{
					maxSteps:     2,
					completeType: "workflow.complete",
				}
			},
			"app.workflows:multi_step": func() process.Workflow {
				return &mockWorkflow{
					maxSteps:     3,
					completeType: "workflow.complete",
				}
			},
		},
	}

	// Create workflow_sandbox module
	module := NewWorkflowSandboxModule()

	// Create VM with coroutine and channel layers
	vm, err := engine.NewCVM(logger)
	require.NoError(t, err)

	L := vm.State()
	L.PreloadModule(module.Info().Name, module.Loader)

	// Load upstream module for command methods - must call Loader to register metatables
	upstreamMod := upstream.NewUpstreamModule()
	upstreamMod.Loader(L) // Call the loader to register metatables
	L.Pop(1)              // Pop the returned module table

	runner := engine.NewRunner(vm,
		engine.WithLayer(coroutine.NewCoroutineLayer()),
		engine.WithLayer(channel.NewChannelLayer()))

	// Setup context with prototype factory
	ctx := ctxapi.NewRootContext()
	ac := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, ac)

	// Add prototype factory to context
	ctx = process.WithPrototypes(ctx, factory)

	ctx, _ = ctxapi.OpenFrameContext(ctx)

	return vm, runner, ctx, factory
}

func TestWorkflowSandboxModule_Name(t *testing.T) {
	mod := NewWorkflowSandboxModule()
	assert.Equal(t, "workflow_sandbox", mod.Info().Name)
}

func TestWorkflowSandboxModule_GetWorkflow(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_get_workflow()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow: " .. tostring(err)) end
			assert(wf ~= nil, "workflow should not be nil")

			wf:close()
			return {success = true}
		end
	`, "test", "test_get_workflow")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_get_workflow")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_SingleStepWorkflow(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_single_step()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow with input
			local ok, err = wf:start("test input")
			if not ok then error("start failed: " .. tostring(err)) end

			-- Execute first step
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			-- Get commands
			local commands = wf:commands()
			assert(#commands == 1, "should have 1 command, got: " .. #commands)

			-- Execute second step (workflow will complete after maxSteps)
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			-- Check if done
			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_single_step")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_single_step")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_MultiStepWorkflow(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_multi_step()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:multi_step")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Step 1: First execution
			local ok, err = wf:step()
			if not ok then error("step 1 failed: " .. tostring(err)) end

			-- Get first timer command
			local commands = wf:commands()
			assert(#commands == 1, "should have 1 command after step 1")

			-- Step 2: Continue after first timer
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			-- Get second timer command
			commands = wf:commands()
			assert(#commands == 1, "should have 1 command after step 2")

			-- Step 3: Complete workflow
			local ok, err = wf:step()
			if not ok then error("step 3 failed: " .. tostring(err)) end

			-- Check completion
			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_multi_step")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_multi_step")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_WorkflowNotFound(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_workflow_not_found()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:nonexistent")
			assert(wf == nil, "workflow should be nil")
			assert(err ~= nil, "should have error")
			assert(string.find(err, "not found"), "error should mention 'not found'")

			return {success = true}
		end
	`, "test", "test_workflow_not_found")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_workflow_not_found")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_CommandRetrieval(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_command_retrieval()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start("data")
			if not ok then error("start failed: " .. tostring(err)) end

			-- Execute workflow
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			-- Get commands
			local commands = wf:commands()
			assert(#commands == 1, "should have 1 command")

			-- Continue workflow
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			wf:close()
			return {success = true}
		end
	`, "test", "test_command_retrieval")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_command_retrieval")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_ResultSuccess(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_result_success()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start("test input")
			if not ok then error("start failed: " .. tostring(err)) end

			-- Execute first step
			local ok, err = wf:step()
			if not ok then error("step 1 failed: " .. tostring(err)) end

			-- Get commands and continue
			local commands = wf:commands()
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			-- Now workflow is done
			assert(wf:done(), "workflow should be done")

			-- Get result
			local result, err = wf:result()

			-- Mock workflow completes successfully but has no result value
			assert(err == nil, "should not have error")

			wf:close()
			return {success = true}
		end
	`, "test", "test_result_success")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_result_success")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_ResultNotDone(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_result_not_done()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start but don't complete workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Try to get result before workflow is done
			local result, err = wf:result()
			assert(result == nil, "result should be nil when not done")
			assert(err ~= nil, "should have error when getting result before done")
			assert(string.find(err, "not done yet"), "error should mention 'not done yet'")

			wf:close()
			return {success = true}
		end
	`, "test", "test_result_not_done")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_result_not_done")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_ErrorDuringStep(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	// Create a workflow that fails on step 2
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:failing"] = func() process.Workflow {
		return &mockWorkflow{
			maxSteps:     3,
			shouldFail:   true,
			completeType: "workflow.complete",
		}
	}

	err := vm.Import(`
		function test_error_during_step()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:failing")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- First step should succeed
			local ok, err = wf:step()
			if not ok then error("step 1 failed: " .. tostring(err)) end

			-- Second step should fail (mock workflow fails on step 2)
			local ok, err = wf:step()
			assert(not ok, "step 2 should fail")
			assert(err ~= nil, "should have error on step 2")
			assert(string.find(err, "mock error"), "error should be mock error")

			wf:close()
			return {success = true}
		end
	`, "test", "test_error_during_step")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_error_during_step")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// mockMultiCommandWorkflow sends 3 commands on first step
type mockMultiCommandWorkflow struct {
	*mockWorkflow
	firstStepDone bool
}

func (w *mockMultiCommandWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	// First step sends 3 commands
	if !w.firstStepDone {
		w.firstStepDone = true

		if w.ctx != nil {
			if upstream, ok := runtime.GetUpstream(w.ctx); ok {
				_ = upstream.SendRequest(newMockCommand("timer.sleep", payload.New(1000)))
				_ = upstream.SendRequest(newMockCommand("timer.sleep", payload.New(2000)))
				_ = upstream.SendRequest(newMockCommand("timer.sleep", payload.New(3000)))
			}
		}

		return process.StepContinue, nil
	}

	// Second step completes
	if w.ctx != nil {
		if upstream, ok := runtime.GetUpstream(w.ctx); ok {
			_ = upstream.SendRequest(newMockCommand(w.completeType))
		}
	}

	w.triggerOnComplete(nil)
	return process.StepDone, nil
}

func TestWorkflowSandboxModule_UpstreamQueueing(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	// Create custom workflow that sends 3 commands in first step
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:multi_command"] = func() process.Workflow {
		return &mockMultiCommandWorkflow{
			mockWorkflow: &mockWorkflow{
				maxSteps:     2,
				completeType: "workflow.complete",
			},
		}
	}

	err := vm.Import(`
		function test_upstream_queueing()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:multi_command")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Execute step
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			-- Get commands (should have 3)
			local commands = wf:commands()
			assert(#commands == 3, "should have 3 commands, got: " .. #commands)

			-- Get commands again (should flush and return empty)
			commands = wf:commands()
			assert(#commands == 0, "should have 0 commands after flush, got: " .. #commands)

			wf:close()
			return {success = true}
		end
	`, "test", "test_upstream_queueing")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_upstream_queueing")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestWorkflowSandboxModule_ParallelWorkflows(t *testing.T) {
	vm, runner, ctx, _ := setupTestEnvironment(t)
	defer vm.Close()

	err := vm.Import(`
		function test_parallel_workflows()
			local workflow_sandbox = require("workflow_sandbox")

			-- Create two workflow instances
			local wf1, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow 1: " .. tostring(err)) end

			local wf2, err = workflow_sandbox.get("app.workflows:simple")
			if err then error("failed to get workflow 2: " .. tostring(err)) end

			-- Start both workflows
			local ok, err = wf1:start("input 1")
			if not ok then error("wf1 start failed: " .. tostring(err)) end

			local ok, err = wf2:start("input 2")
			if not ok then error("wf2 start failed: " .. tostring(err)) end

			-- Step both workflows independently
			local ok, err = wf1:step()
			if not ok then error("wf1 step failed: " .. tostring(err)) end

			local ok, err = wf2:step()
			if not ok then error("wf2 step failed: " .. tostring(err)) end

			-- Verify both have commands
			local commands1 = wf1:commands()
			local commands2 = wf2:commands()

			assert(#commands1 == 1, "wf1 should have 1 command")
			assert(#commands2 == 1, "wf2 should have 1 command")

			-- Check done states are independent
			assert(not wf1:done(), "wf1 should not be done yet")
			assert(not wf2:done(), "wf2 should not be done yet")

			wf1:close()
			wf2:close()
			return {success = true}
		end
	`, "test", "test_parallel_workflows")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_parallel_workflows")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// mockUpstreamSendWorkflow uses upstream.send() directly from Lua code
type mockUpstreamSendWorkflow struct {
	mu           sync.Mutex
	started      bool
	stepCount    int
	ctx          context.Context
	pid          relay.PID
	sendCount    int
	maxSteps     int
	completeType string
}

func (w *mockUpstreamSendWorkflow) Start(ctx context.Context, pid relay.PID, _ payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.ctx = ctx
	w.pid = pid
	return nil
}

func (w *mockUpstreamSendWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	if w.stepCount < w.maxSteps {
		if w.ctx != nil {
			if upstream, ok := runtime.GetUpstream(w.ctx); ok {
				cmd := newMockCommand("activity.execute", payload.New(w.sendCount))
				_ = upstream.SendRequest(cmd)
				w.sendCount++
			}
		}
		return process.StepContinue, nil
	}

	// Complete workflow
	if w.ctx != nil {
		hooks := process.GetOnCompleteHooks(w.ctx)
		if hooks != nil {
			result := &runtime.Result{Value: payload.New("workflow completed")}
			for _, hook := range hooks {
				hook(w.ctx, w.pid, result)
			}
		}
	}
	return process.StepDone, nil
}

func (w *mockUpstreamSendWorkflow) Commands() []runtime.Command {
	return nil
}

func (w *mockUpstreamSendWorkflow) Send(_ *relay.Package) error {
	return nil
}

func (w *mockUpstreamSendWorkflow) Terminate() {}

func TestWorkflowSandboxModule_UpstreamSend(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	// Register workflow that uses upstream.send
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:upstream_send"] = func() process.Workflow {
		return &mockUpstreamSendWorkflow{
			maxSteps:     3,
			completeType: "workflow.complete",
		}
	}

	err := vm.Import(`
		function test_upstream_send()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:upstream_send")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start("test")
			if not ok then error("start failed: " .. tostring(err)) end

			-- Execute steps and verify commands are queued via upstream
			local ok, err = wf:step()
			if not ok then error("step 1 failed: " .. tostring(err)) end

			local commands = wf:commands()
			assert(#commands == 1, "step 1: expected 1 command, got: " .. #commands)

			-- Access command properties via runtime.Command interface methods
			local cmd = commands[1]
			assert(cmd ~= nil, "command should not be nil")
			assert(cmd:type() == "activity.execute", "command type should be 'activity.execute', got: " .. tostring(cmd:type()))
			assert(cmd:id() ~= nil, "command id should not be nil")

			-- Step 2
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			commands = wf:commands()
			assert(#commands == 1, "step 2: expected 1 command, got: " .. #commands)

			-- Step 3 (completes)
			local ok, err = wf:step()
			if not ok then error("step 3 failed: " .. tostring(err)) end

			assert(wf:done(), "workflow should be done after step 3")

			wf:close()
			return {success = true}
		end
	`, "test", "test_upstream_send")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_upstream_send")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// mockProcessSendWorkflow emits process.send commands
type mockProcessSendWorkflow struct {
	mu        sync.Mutex
	started   bool
	stepCount int
	ctx       context.Context
	pid       relay.PID
	maxSteps  int
}

func (w *mockProcessSendWorkflow) Start(ctx context.Context, pid relay.PID, _ payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.ctx = ctx
	w.pid = pid
	return nil
}

func (w *mockProcessSendWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	if w.stepCount < w.maxSteps {
		if w.ctx != nil {
			if upstream, ok := runtime.GetUpstream(w.ctx); ok {
				// Create process.send command with destination and topic metadata
				cmd := newMockCommand("process.send",
					payload.New(map[string]string{"destination": "target_pid", "topic": "greeting"}),
					payload.New("hello world"),
				)
				_ = upstream.SendRequest(cmd)
			}
		}
		return process.StepContinue, nil
	}

	// Complete workflow
	if w.ctx != nil {
		hooks := process.GetOnCompleteHooks(w.ctx)
		if hooks != nil {
			result := &runtime.Result{Value: payload.New("done")}
			for _, hook := range hooks {
				hook(w.ctx, w.pid, result)
			}
		}
	}
	return process.StepDone, nil
}

func (w *mockProcessSendWorkflow) Commands() []runtime.Command {
	return nil
}

func (w *mockProcessSendWorkflow) Send(_ *relay.Package) error {
	return nil
}

func (w *mockProcessSendWorkflow) Terminate() {}

func TestWorkflowSandboxModule_ProcessSend(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	// Register workflow that uses process.send
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:process_send"] = func() process.Workflow {
		return &mockProcessSendWorkflow{
			maxSteps: 2,
		}
	}

	err := vm.Import(`
		function test_process_send()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:process_send")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Step triggers process.send command
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			-- Verify process.send command
			local commands = wf:commands()
			assert(#commands == 1, "expected 1 command, got: " .. #commands)

			local cmd = commands[1]
			assert(cmd:type() == "process.send", "expected process.send command, got: " .. cmd:type())
			assert(cmd:id() ~= nil, "command id should not be nil")

			-- Get params
			local params = cmd:params()
			assert(#params >= 2, "expected at least 2 params")

			-- Complete workflow
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_process_send")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_process_send")
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// mockReceivingWorkflow can receive messages via Send()
type mockReceivingWorkflow struct {
	mu           sync.Mutex
	started      bool
	stepCount    int
	ctx          context.Context
	pid          relay.PID
	maxSteps     int
	receivedMsgs []*relay.Package
}

func (w *mockReceivingWorkflow) Start(ctx context.Context, pid relay.PID, _ payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.ctx = ctx
	w.pid = pid
	w.receivedMsgs = make([]*relay.Package, 0)
	return nil
}

func (w *mockReceivingWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	if w.stepCount >= w.maxSteps {
		// Complete workflow
		if w.ctx != nil {
			hooks := process.GetOnCompleteHooks(w.ctx)
			if hooks != nil {
				result := &runtime.Result{Value: payload.New(len(w.receivedMsgs))}
				for _, hook := range hooks {
					hook(w.ctx, w.pid, result)
				}
			}
		}
		return process.StepDone, nil
	}

	return process.StepContinue, nil
}

func (w *mockReceivingWorkflow) Commands() []runtime.Command {
	return nil
}

func (w *mockReceivingWorkflow) Send(pkg *relay.Package) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.receivedMsgs = append(w.receivedMsgs, pkg)
	return nil
}

func (w *mockReceivingWorkflow) Terminate() {}

func (w *mockReceivingWorkflow) ReceivedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.receivedMsgs)
}

func TestWorkflowSandboxModule_SendMessage(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	var receivingWorkflow *mockReceivingWorkflow

	// Register workflow that receives messages
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:receiving"] = func() process.Workflow {
		receivingWorkflow = &mockReceivingWorkflow{
			maxSteps: 3,
		}
		return receivingWorkflow
	}

	err := vm.Import(`
		function test_send_message()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:receiving")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Step 1
			local ok, err = wf:step()
			if not ok then error("step 1 failed: " .. tostring(err)) end

			-- Send message to workflow (queues it)
			local ok, err = wf:send("my_topic", "hello", 123)
			if not ok then error("send failed: " .. tostring(err)) end

			-- Step 2 (processes queued message)
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			-- Step 3 (completes)
			local ok, err = wf:step()
			if not ok then error("step 3 failed: " .. tostring(err)) end

			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_send_message")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_send_message")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify the workflow received the message
	assert.Equal(t, 1, receivingWorkflow.ReceivedCount(), "workflow should have received 1 message")
}

// mockProcessIDWorkflow verifies that process.id() and process.pid() work correctly
type mockProcessIDWorkflow struct {
	mu        sync.Mutex
	started   bool
	stepCount int
	ctx       context.Context
	pid       relay.PID
	maxSteps  int
	// Store what we got from context for verification
	gotID  string
	gotPID string
}

func (w *mockProcessIDWorkflow) Start(ctx context.Context, pid relay.PID, _ payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.ctx = ctx
	w.pid = pid

	// Check frame context for ID and PID values
	fc := ctxapi.FrameFromContext(ctx)
	if fc != nil {
		if idVal, ok := fc.Get(runtime.FrameIDKey); ok {
			if id, ok := idVal.(registry.ID); ok {
				w.gotID = id.String()
			}
		}
		if pidVal, ok := runtime.GetFramePID(ctx); ok {
			w.gotPID = pidVal.String()
		}
	}

	return nil
}

func (w *mockProcessIDWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return process.StepContinue, fmt.Errorf("not started")
	}

	w.stepCount++

	if w.stepCount >= w.maxSteps {
		// Complete workflow with ID and PID in result
		if w.ctx != nil {
			hooks := process.GetOnCompleteHooks(w.ctx)
			if hooks != nil {
				result := &runtime.Result{
					Value: payload.New(map[string]string{
						"id":  w.gotID,
						"pid": w.gotPID,
					}),
				}
				for _, hook := range hooks {
					hook(w.ctx, w.pid, result)
				}
			}
		}
		return process.StepDone, nil
	}

	return process.StepContinue, nil
}

func (w *mockProcessIDWorkflow) Commands() []runtime.Command {
	return nil
}

func (w *mockProcessIDWorkflow) Send(_ *relay.Package) error {
	return nil
}

func (w *mockProcessIDWorkflow) Terminate() {}

func (w *mockProcessIDWorkflow) GetGotID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gotID
}

func (w *mockProcessIDWorkflow) GetGotPID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gotPID
}

func TestWorkflowSandboxModule_FrameContext(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	var processIDWorkflow *mockProcessIDWorkflow

	// Register workflow that checks frame context
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:process_id"] = func() process.Workflow {
		processIDWorkflow = &mockProcessIDWorkflow{
			maxSteps: 1,
		}
		return processIDWorkflow
	}

	err := vm.Import(`
		function test_frame_context()
			local workflow_sandbox = require("workflow_sandbox")

			local wf, err = workflow_sandbox.get("app.workflows:process_id")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Step to completion
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_frame_context")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_frame_context")
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify that workflow got proper ID and PID from frame context
	assert.Equal(t, "app.workflows:process_id", processIDWorkflow.GetGotID(), "workflow should see correct registry ID")
	assert.NotEmpty(t, processIDWorkflow.GetGotPID(), "workflow should see non-empty PID")
}

// mockTaskWorkflow receives tasks via PushTask and processes them
type mockTaskWorkflow struct {
	mu           sync.Mutex
	started      bool
	stepCount    int
	ctx          context.Context
	pid          relay.PID
	maxSteps     int
	receivedTask std.Task
}

func (w *mockTaskWorkflow) Start(ctx context.Context, pid relay.PID, _ payload.Payloads) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.started = true
	w.ctx = ctx
	w.pid = pid
	return nil
}

func (w *mockTaskWorkflow) Step() (process.StepResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stepCount++

	if w.stepCount > w.maxSteps {
		if w.ctx != nil {
			hooks := process.GetOnCompleteHooks(w.ctx)
			if hooks != nil {
				result := &runtime.Result{Value: payload.New("done")}
				for _, hook := range hooks {
					hook(w.ctx, w.pid, result)
				}
			}
		}
		return process.StepDone, nil
	}

	// On first step, process received task if any
	if w.receivedTask != nil && w.stepCount == 1 {
		w.receivedTask.Complete(payload.NewPayload("task completed: "+w.receivedTask.Type(), payload.Golang))
	}

	return process.StepContinue, nil
}

func (w *mockTaskWorkflow) Commands() []runtime.Command {
	return nil
}

func (w *mockTaskWorkflow) Send(pkg *relay.Package) error {
	return nil
}

func (w *mockTaskWorkflow) Terminate() {}

func (w *mockTaskWorkflow) PushTask(task std.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.receivedTask = task
	return nil
}

func TestWorkflowSandboxModule_MockTask(t *testing.T) {
	vm, runner, ctx, factory := setupTestEnvironment(t)
	defer vm.Close()

	// Register workflow that processes tasks
	mockFactory := factory.(*mockPrototypeFactory)
	mockFactory.prototypes["app.workflows:task_handler"] = func() process.Workflow {
		return &mockTaskWorkflow{
			maxSteps: 1,
		}
	}

	err := vm.Import(`
		function test_mock_task()
			local workflow_sandbox = require("workflow_sandbox")

			-- Create a task
			local task = workflow_sandbox.new_task("query", "get_state")
			assert(task ~= nil, "task should not be nil")
			assert(task:type() == "query", "task type should be 'query'")
			assert(task:input() == "get_state", "task input should be 'get_state'")
			assert(task:completed() == false, "task should not be completed yet")

			-- Get workflow
			local wf, err = workflow_sandbox.get("app.workflows:task_handler")
			if err then error("failed to get workflow: " .. tostring(err)) end

			-- Start workflow
			local ok, err = wf:start()
			if not ok then error("start failed: " .. tostring(err)) end

			-- Push task to workflow
			local ok, err = wf:push_task(task)
			if not ok then error("push_task failed: " .. tostring(err)) end

			-- Step to process the task
			local ok, err = wf:step()
			if not ok then error("step failed: " .. tostring(err)) end

			-- Check task was completed
			assert(task:completed(), "task should be completed after step")

			-- Get task result
			local result, err = task:result()
			assert(err == nil, "task result should not have error: " .. tostring(err))
			assert(result == "task completed: query", "unexpected result: " .. tostring(result))

			-- Complete workflow
			local ok, err = wf:step()
			if not ok then error("step 2 failed: " .. tostring(err)) end

			assert(wf:done(), "workflow should be done")

			wf:close()
			return {success = true}
		end
	`, "test", "test_mock_task")
	require.NoError(t, err)

	result, err := runner.Execute(ctx, "test_mock_task")
	require.NoError(t, err)
	assert.NotNil(t, result)
}
