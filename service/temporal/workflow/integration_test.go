package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	enginepayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/worker"
	"github.com/wippyai/runtime/service/temporal/workflow"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunc "github.com/wippyai/runtime/system/function"
	sysprocess "github.com/wippyai/runtime/system/process"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.uber.org/zap"
)

const helloWorkflowSource = `
local function main(input)
    local name = "World"
    if input ~= nil and input.name ~= nil then
        name = input.name
    end

    return {
        message = string.format("Hello, %s!", name),
        status = "completed"
    }
end

return main
`

// TestWorkflowExecution_Integration tests that Lua workflows can be executed via Temporal.
func TestWorkflowExecution_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Set up code manager for Lua compilation
	codeManager, err := code.NewCodeManager(logger, nil, code.Config{
		Modules:        nil,
		ProtoCacheSize: 100,
		MainCacheSize:  100,
	})
	require.NoError(t, err)

	// Set up process factory for creating Lua processes
	processFactory := engine.NewProcessFactory(codeManager)

	// Set up factory registry
	factoryRegistry := sysprocess.NewFactoryRegistry(bus, logger.Named("factory"))

	// Set up function registry
	funcRegistry := sysfunc.NewFunctionRegistry(bus, logger.Named("function"))

	// Set up root context with all dependencies
	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, funcRegistry)
	ctx = process.WithFactory(ctx, factoryRegistry)

	// Set up PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	// Set up transcoder for Lua<->JSON conversion
	payload.WithTranscoder(ctx, newTestTranscoder())

	// Start registries
	require.NoError(t, funcRegistry.Start(ctx))
	defer func() { _ = funcRegistry.Stop() }()

	require.NoError(t, factoryRegistry.Start(ctx))
	defer func() { _ = factoryRegistry.Stop() }()

	// Add workflow code to code manager
	workflowID := registry.NewID("test.workflow", "hello")
	node := code.Node{
		ID:     workflowID,
		Kind:   luaapi.Workflow,
		Source: helloWorkflowSource,
		Method: "main",
	}
	require.NoError(t, codeManager.AddNode(ctx, node, nil))

	// Create factory for the workflow with deterministic module restrictions
	factoryFn, err := processFactory.CreateFactory(workflowID,
		engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow),
	)
	require.NoError(t, err)

	// Register factory with the factory registry
	awaiter := eventbus.NewAwaiter(bus, process.System, "factory.(accept|reject)")
	waiter, err := awaiter.Prepare(ctx, workflowID.String())
	require.NoError(t, err)

	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   workflowID.String(),
		Data: &process.FactoryEntry{
			Factory: factoryFn,
			Meta:    process.Meta{Method: "main"},
		},
	})

	result := waiter.Wait()
	require.True(t, result.Accepted, "factory should be accepted")

	// Create data converter
	dc := dataconverter.NewDataConverter(newTestTranscoder(), converter.GetDefaultDataConverter())

	// Start Temporal test server
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	temporalClient := server.Client()
	defer temporalClient.Close()

	// Create mock resource registry with the client
	resourceReg := newTestResourceRegistry()
	clientResource := api.ClientResource{
		Client: temporalClient,
	}
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = clientResource

	// Create wippy worker with workflow support
	taskQueue := "test-workflow-queue"
	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: taskQueue,
		WorkerOptions: api.WorkerOptionsConfig{
			MaxConcurrentWorkflowTaskExecutionSize: 10,
		},
	}

	wippyWorker := worker.NewWorker(
		logger,
		registry.NewID("test", "worker"),
		workerConfig,
		resourceReg,
		nil,
	)

	// Create and register workflow definition factory
	defFactory := &workflow.DefinitionFactory{
		ID: workflowID,
	}

	workflowName := workflowID.String()
	require.NoError(t, wippyWorker.RegisterWorkflow(ctx, workflowName, defFactory))

	// Start the worker
	statusCh, err := wippyWorker.Start(ctx)
	require.NoError(t, err)

	// Wait for worker to be running
	status := <-statusCh
	require.NotNil(t, status)

	defer func() { _ = wippyWorker.Stop(ctx) }()

	// Execute workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "workflow-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]interface{}{
		"name": "Temporal",
	}

	we, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, workflowName, testInput)
	require.NoError(t, err)

	var workflowResult map[string]interface{}
	err = we.Get(ctx, &workflowResult)
	require.NoError(t, err)

	// Verify result
	require.Equal(t, "Hello, Temporal!", workflowResult["message"])
	require.Equal(t, "completed", workflowResult["status"])
}

// testTranscoder implements payload.Transcoder for testing using real Lua<->JSON conversion
type testTranscoder struct {
	luaToJSON *enginepayload.ToJSON
	jsonToLua *enginepayload.JSONToLua
}

func newTestTranscoder() *testTranscoder {
	return &testTranscoder{
		luaToJSON: &enginepayload.ToJSON{},
		jsonToLua: &enginepayload.JSONToLua{},
	}
}

func (m *testTranscoder) Transcode(p payload.Payload, target payload.Format) (payload.Payload, error) {
	if p.Format() == payload.Lua && target == payload.JSON {
		return m.luaToJSON.Transcode(p)
	}
	if p.Format() == payload.JSON && target == payload.Lua {
		return m.jsonToLua.Transcode(p)
	}
	return p, nil
}

func (m *testTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return nil
}

// testResourceRegistry provides a mock resource registry for testing
type testResourceRegistry struct {
	resources map[registry.ID]any
}

func newTestResourceRegistry() *testResourceRegistry {
	return &testResourceRegistry{
		resources: make(map[registry.ID]any),
	}
}

func (m *testResourceRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	val, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &testResource{value: val}, nil
}

func (m *testResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *testResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

type testResource struct {
	value any
}

func (r *testResource) Get() (any, error) { return r.value, nil }
func (r *testResource) Release()          {}

const concurrentWorkflowSource = `
local time = require("time")

local function main(input)
    local worker_count = 3
    local job_count = 6

    if input ~= nil then
        worker_count = input.workers or worker_count
        job_count = input.jobs or job_count
    end

    local work_queue = channel.new(10)
    local results = channel.new(10)

    for w = 1, worker_count do
        coroutine.spawn(function()
            while true do
                local job, ok = work_queue:receive()
                if not ok then break end
                time.sleep(10 * time.MILLISECOND)
                results:send({worker = w, job = job, result = job * 2})
            end
        end)
    end

    for i = 1, job_count do
        work_queue:send(i)
    end
    work_queue:close()

    local total = 0
    local processed = {}
    for i = 1, job_count do
        local r = results:receive()
        total = total + r.result
        table.insert(processed, r)
    end

    return {
        total = total,
        job_count = job_count,
        worker_count = worker_count
    }
end

return main
`

// TestConcurrentWorkflow_Integration tests workflow with coroutines, channels, and timers.
func TestConcurrentWorkflow_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	codeManager, err := code.NewCodeManager(logger, nil, code.Config{
		Modules:        []luaapi.Module{timemod.Module},
		ProtoCacheSize: 100,
		MainCacheSize:  100,
	})
	require.NoError(t, err)

	processFactory := engine.NewProcessFactory(codeManager)
	factoryRegistry := sysprocess.NewFactoryRegistry(bus, logger.Named("factory"))
	funcRegistry := sysfunc.NewFunctionRegistry(bus, logger.Named("function"))

	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, funcRegistry)
	ctx = process.WithFactory(ctx, factoryRegistry)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	payload.WithTranscoder(ctx, newTestTranscoder())

	require.NoError(t, funcRegistry.Start(ctx))
	defer func() { _ = funcRegistry.Stop() }()

	require.NoError(t, factoryRegistry.Start(ctx))
	defer func() { _ = factoryRegistry.Stop() }()

	workflowID := registry.NewID("test.workflow", "concurrent")
	node := code.Node{
		ID:     workflowID,
		Kind:   luaapi.Workflow,
		Source: concurrentWorkflowSource,
		Method: "main",
	}
	imports := []code.Import{{ID: registry.NewID("", "time"), Alias: "time"}}
	require.NoError(t, codeManager.AddNode(ctx, node, imports))

	factoryFn, err := processFactory.CreateFactory(workflowID,
		engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow),
	)
	require.NoError(t, err)

	awaiter := eventbus.NewAwaiter(bus, process.System, "factory.(accept|reject)")
	waiter, err := awaiter.Prepare(ctx, workflowID.String())
	require.NoError(t, err)

	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   workflowID.String(),
		Data: &process.FactoryEntry{
			Factory: factoryFn,
			Meta:    process.Meta{Method: "main"},
		},
	})

	result := waiter.Wait()
	require.True(t, result.Accepted, "factory should be accepted")

	dc := dataconverter.NewDataConverter(newTestTranscoder(), converter.GetDefaultDataConverter())

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	temporalClient := server.Client()
	defer temporalClient.Close()

	resourceReg := newTestResourceRegistry()
	clientResource := api.ClientResource{
		Client: temporalClient,
	}
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = clientResource

	taskQueue := "test-concurrent-queue"
	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: taskQueue,
		WorkerOptions: api.WorkerOptionsConfig{
			MaxConcurrentWorkflowTaskExecutionSize: 10,
		},
	}

	wippyWorker := worker.NewWorker(
		logger,
		registry.NewID("test", "worker"),
		workerConfig,
		resourceReg,
		nil,
	)

	defFactory := &workflow.DefinitionFactory{
		ID: workflowID,
	}

	workflowName := workflowID.String()
	require.NoError(t, wippyWorker.RegisterWorkflow(ctx, workflowName, defFactory))

	statusCh, err := wippyWorker.Start(ctx)
	require.NoError(t, err)

	status := <-statusCh
	require.NotNil(t, status)

	defer func() { _ = wippyWorker.Stop(ctx) }()

	workflowOptions := client.StartWorkflowOptions{
		ID:        "concurrent-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]interface{}{
		"workers": 3,
		"jobs":    6,
	}

	we, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, workflowName, testInput)
	require.NoError(t, err)

	getCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var workflowResult map[string]interface{}
	err = we.Get(getCtx, &workflowResult)
	require.NoError(t, err)

	// sum of (1+2+3+4+5+6)*2 = 42
	require.Equal(t, float64(42), workflowResult["total"])
	require.Equal(t, float64(6), workflowResult["job_count"])
	require.Equal(t, float64(3), workflowResult["worker_count"])
}
