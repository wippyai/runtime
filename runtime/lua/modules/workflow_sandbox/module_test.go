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
	L.PreloadModule(module.Name(), module.Loader)

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
	assert.Equal(t, "workflow_sandbox", mod.Name())
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
