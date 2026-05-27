// SPDX-License-Identifier: MPL-2.0

package worker_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	enginepayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/worker"
	"github.com/wippyai/runtime/service/temporal/workflow"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunc "github.com/wippyai/runtime/system/function"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	sysprocess "github.com/wippyai/runtime/system/process"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.uber.org/zap"
)

// Workflow that waits for a signal from Worker.Send
const signalFromLocalWorkflowSource = `
local time = require("time")
local channel = require("channel")
local process = require("process")

local function main(input)
    local timeout_ms = input and input.timeout or 5000

    local data_ch, err = process.listen("data", {message = true})
    if err then
        return { status = "listen_error", error = tostring(err) }
    end

    local timeout_ch = time.after(timeout_ms * time.MILLISECOND)

    local result = channel.select{
        data_ch:case_receive(),
        timeout_ch:case_receive()
    }

    if result.channel == timeout_ch then
        return { status = "timeout" }
    end

    local msg = result.value
    local p = msg:payload()
    local data = p:data()
    return {
        status = "received",
        topic = msg:topic(),
        payload = data
    }
end

return main
`

// TestWorkerSend_Integration tests that Worker.Send delivers messages to workflows via signals.
func TestWorkerSend_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	codeManager, err := code.NewCodeManager(logger, nil, code.Config{
		Modules:        []*luaapi.ModuleDef{timemod.Module, processmod.Module},
		ProtoCacheSize: 100,
		MainCacheSize:  100,
	})
	require.NoError(t, err)

	processFactory := engine.NewProcessFactory(codeManager)
	factoryRegistry := sysprocess.NewFactoryRegistry(bus, logger.Named("factory"))
	funcRegistry := sysfunc.NewFunctionRegistry(bus, logger.Named("function"))

	ctx := ctxapi.NewRootContext()
	awaitSvc := eventbus.NewAwaitService(bus)
	require.NoError(t, awaitSvc.Start(ctx))
	defer func() { _ = awaitSvc.Stop() }()
	ctx = function.WithRegistry(ctx, funcRegistry)
	ctx = process.WithFactory(ctx, factoryRegistry)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	payload.WithTranscoder(ctx, newSendTestTranscoder())

	require.NoError(t, funcRegistry.Start(ctx))
	defer func() { _ = funcRegistry.Stop() }()

	require.NoError(t, factoryRegistry.Start(ctx))
	defer func() { _ = factoryRegistry.Stop() }()

	workflowID := registry.NewID("test.workflow", "signal_from_local")
	node := code.Node{
		ID:     workflowID,
		Kind:   luaapi.Workflow,
		Source: signalFromLocalWorkflowSource,
		Method: "main",
	}
	imports := []code.Import{
		{ID: registry.NewID("", "time"), Alias: "time"},
		{ID: registry.NewID("", "process"), Alias: "process"},
	}
	require.NoError(t, codeManager.AddNode(ctx, node, imports))

	factoryFn, err := processFactory.CreateFactory(workflowID,
		engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow, luaapi.ClassProcess, luaapi.ClassTime),
		engine.WithModule(processmod.Module),
	)
	require.NoError(t, err)

	waiter, err := awaitSvc.Prepare(ctx, process.System, "factory.(accept|reject)", workflowID.String(), 0)
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

	dc := dataconverter.NewDataConverter(newSendTestTranscoder())

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	temporalClient := server.Client()
	defer temporalClient.Close()

	resourceReg := newSendTestResourceRegistry()
	clientResource := api.ClientResource{
		Client: temporalClient,
	}
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = clientResource

	taskQueue := "test-worker-send-queue"
	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: taskQueue,
		WorkerOptions: api.WorkerOptionsConfig{
			MaxConcurrentWorkflowTaskExecutionSize: 10,
		},
	}

	wippyWorker, err := worker.NewWorkerBuilder().
		WithLogger(logger).
		WithID(registry.NewID("test", "worker")).
		WithConfig(workerConfig).
		WithResourceRegistry(resourceReg).
		WithTranscoder(newSendTestTranscoder()).
		Build()
	require.NoError(t, err)

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

	// Start workflow that waits for signal
	workflowOptions := client.StartWorkflowOptions{
		ID:        "worker-send-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]any{
		"timeout": 10000,
	}

	we, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, workflowName, testInput)
	require.NoError(t, err)

	// Let workflow start and begin listening
	time.Sleep(500 * time.Millisecond)

	// Send message via Worker.Send (simulating a local process sending to the workflow)
	pkg := &relay.Package{
		Source: pid.PID{Node: "local", Host: "test", UniqID: "sender-1"},
		Target: pid.PID{Node: "temporal", Host: taskQueue, UniqID: we.GetID()},
		Messages: []*relay.Message{
			{
				Topic:    "data",
				Payloads: []payload.Payload{payload.New(map[string]any{"key": "value", "num": 42})},
			},
		},
	}

	err = wippyWorker.Send(pkg)
	require.NoError(t, err)

	// Wait for workflow to complete
	getCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var workflowResult map[string]any
	err = we.Get(getCtx, &workflowResult)
	require.NoError(t, err)

	// Verify workflow received the signal
	require.Equal(t, "received", workflowResult["status"])
	require.Equal(t, "data", workflowResult["topic"])

	// Verify payload was received
	receivedPayload, ok := workflowResult["payload"].(map[string]any)
	require.True(t, ok, "payload should be a map")
	require.Equal(t, "value", receivedPayload["key"])
	switch v := receivedPayload["num"].(type) {
	case int:
		require.Equal(t, 42, v)
	case int64:
		require.Equal(t, int64(42), v)
	case float64:
		require.Equal(t, float64(42), v)
	default:
		t.Fatalf("unexpected num type: %T", receivedPayload["num"])
	}
}

// TestWorkerSend_ClosedWorker verifies Send returns error when worker is closed.
func TestWorkerSend_ClosedWorker(t *testing.T) {
	logger := zap.NewNop()
	workerConfig := &api.WorkerConfig{
		Client:    registry.NewID("test", "client"),
		TaskQueue: "test-queue",
	}

	wippyWorker, err := worker.NewWorkerBuilder().
		WithLogger(logger).
		WithID(registry.NewID("test", "worker")).
		WithConfig(workerConfig).
		WithTranscoder(newSendTestTranscoder()).
		Build()
	require.NoError(t, err)

	// Worker is not started, so temporalClient is nil
	pkg := &relay.Package{
		Target: pid.PID{UniqID: "workflow-1"},
		Messages: []*relay.Message{
			{Topic: "test", Payloads: []payload.Payload{payload.New("data")}},
		},
	}

	err = wippyWorker.Send(pkg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "temporal client not available")
}

// TestWorkerSend_EmptyWorkflowID verifies Send returns error for empty workflow ID.
func TestWorkerSend_EmptyWorkflowID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()
	ctx := ctxapi.NewRootContext()

	dc := dataconverter.NewDataConverter(newSendTestTranscoder())

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	temporalClient := server.Client()
	defer temporalClient.Close()

	resourceReg := newSendTestResourceRegistry()
	clientResource := api.ClientResource{
		Client: temporalClient,
	}
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = clientResource

	funcRegistry := sysfunc.NewFunctionRegistry(bus, logger.Named("function"))
	ctx = function.WithRegistry(ctx, funcRegistry)
	require.NoError(t, funcRegistry.Start(ctx))
	defer func() { _ = funcRegistry.Stop() }()

	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: "test-queue",
	}

	wippyWorker, err := worker.NewWorkerBuilder().
		WithLogger(logger).
		WithID(registry.NewID("test", "worker")).
		WithConfig(workerConfig).
		WithResourceRegistry(resourceReg).
		WithTranscoder(newSendTestTranscoder()).
		Build()
	require.NoError(t, err)

	statusCh, err := wippyWorker.Start(ctx)
	require.NoError(t, err)
	<-statusCh
	defer func() { _ = wippyWorker.Stop(ctx) }()

	// Send with empty workflow ID
	pkg := &relay.Package{
		Target: pid.PID{UniqID: ""},
		Messages: []*relay.Message{
			{Topic: "test", Payloads: []payload.Payload{payload.New("data")}},
		},
	}

	err = wippyWorker.Send(pkg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "target workflow ID is empty")
}

// newSendTestTranscoder creates a transcoder with all necessary format conversions
func newSendTestTranscoder() payload.Transcoder {
	transcoder := syspayload.NewTranscoder()

	// Register Lua ↔ JSON conversions
	transcoder.RegisterTranscoder(payload.Lua, payload.JSON, 1, &enginepayload.ToJSON{})
	transcoder.RegisterTranscoder(payload.JSON, payload.Lua, 1, &enginepayload.JSONToLua{})

	// Register Golang ↔ JSON conversions
	jsonpayload.Register(transcoder)
	msgpayload.Register(transcoder)

	return transcoder
}

// sendTestResourceRegistry provides a mock resource registry for testing
type sendTestResourceRegistry struct {
	resources map[registry.ID]any
}

func newSendTestResourceRegistry() *sendTestResourceRegistry {
	return &sendTestResourceRegistry{
		resources: make(map[registry.ID]any),
	}
}

func (m *sendTestResourceRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	val, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &sendTestResource{value: val}, nil
}

func (m *sendTestResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.resources))
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *sendTestResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

type sendTestResource struct {
	value any
}

func (r *sendTestResource) Get() (any, error) { return r.value, nil }
func (r *sendTestResource) Release()          {}
