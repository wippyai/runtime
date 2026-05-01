// SPDX-License-Identifier: MPL-2.0

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
	cryptomod "github.com/wippyai/runtime/runtime/lua/modules/crypto"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	uuidmod "github.com/wippyai/runtime/runtime/lua/modules/uuid"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/worker"
	"github.com/wippyai/runtime/service/temporal/workflow"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunc "github.com/wippyai/runtime/system/function"
	syspayload "github.com/wippyai/runtime/system/payload"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	sysprocess "github.com/wippyai/runtime/system/process"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.uber.org/zap"
)

// workflowTestFixture bundles the shared infrastructure for workflow integration tests.
type workflowTestFixture struct {
	t              *testing.T
	ctx            context.Context
	temporalClient client.Client
	taskQueue      string
	wippyWorker    *worker.Worker
	cleanups       []func()
}

// workflowTestOpts configures a workflow integration test.
type workflowTestOpts struct {
	workflowID     registry.ID
	source         string
	taskQueue      string
	modules        []*luaapi.ModuleDef
	allowedClasses []string
	engineOpts     []engine.FactoryOption
	imports        []code.Import
}

func newWorkflowTestFixture(t *testing.T, opts workflowTestOpts) *workflowTestFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	f := &workflowTestFixture{t: t, taskQueue: opts.taskQueue}

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	codeManager, err := code.NewCodeManager(logger, nil, code.Config{
		Modules:        opts.modules,
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
	f.cleanups = append(f.cleanups, func() { _ = awaitSvc.Stop() })
	ctx = function.WithRegistry(ctx, funcRegistry)
	ctx = process.WithFactory(ctx, factoryRegistry)

	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	transcoder := newTestTranscoder()
	payload.WithTranscoder(ctx, transcoder)

	require.NoError(t, funcRegistry.Start(ctx))
	f.cleanups = append(f.cleanups, func() { _ = funcRegistry.Stop() })

	require.NoError(t, factoryRegistry.Start(ctx))
	f.cleanups = append(f.cleanups, func() { _ = factoryRegistry.Stop() })

	node := code.Node{
		ID:     opts.workflowID,
		Kind:   luaapi.Workflow,
		Source: opts.source,
		Method: "main",
	}
	require.NoError(t, codeManager.AddNode(ctx, node, opts.imports))

	classes := opts.allowedClasses
	if len(classes) == 0 {
		classes = []string{luaapi.ClassDeterministic, luaapi.ClassWorkflow}
	}
	factoryOpts := []engine.FactoryOption{
		engine.WithAllowedClasses(classes...),
	}
	factoryOpts = append(factoryOpts, opts.engineOpts...)

	factoryFn, err := processFactory.CreateFactory(opts.workflowID, factoryOpts...)
	require.NoError(t, err)

	waiter, err := awaitSvc.Prepare(ctx, process.System, "factory.(accept|reject)", opts.workflowID.String(), 0)
	require.NoError(t, err)

	bus.Send(ctx, event.Event{
		System: process.System,
		Kind:   process.FactoryRegister,
		Path:   opts.workflowID.String(),
		Data: &process.FactoryEntry{
			Factory: factoryFn,
			Meta:    process.Meta{Method: "main"},
		},
	})

	result := waiter.Wait()
	require.True(t, result.Accepted, "factory should be accepted")

	dc := dataconverter.NewDataConverter(transcoder)

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	f.cleanups = append(f.cleanups, func() { _ = server.Stop() })

	f.temporalClient = server.Client()
	f.cleanups = append(f.cleanups, func() { f.temporalClient.Close() })

	resourceReg := newTestResourceRegistry()
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = api.ClientResource{Client: f.temporalClient}

	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: opts.taskQueue,
		WorkerOptions: api.WorkerOptionsConfig{
			MaxConcurrentWorkflowTaskExecutionSize: 10,
		},
	}

	wippyWorker, err := worker.NewWorkerBuilder().
		WithLogger(logger).
		WithID(registry.NewID("test", "worker")).
		WithConfig(workerConfig).
		WithResourceRegistry(resourceReg).
		WithTranscoder(transcoder).
		Build()
	require.NoError(t, err)

	workflowName := opts.workflowID.String()
	require.NoError(t, wippyWorker.RegisterWorkflow(ctx, workflowName, &workflow.DefinitionFactory{
		ID: opts.workflowID,
	}))

	statusCh, err := wippyWorker.Start(ctx)
	require.NoError(t, err)
	<-statusCh
	f.cleanups = append(f.cleanups, func() { _ = wippyWorker.Stop(ctx) })

	f.ctx = ctx
	f.wippyWorker = wippyWorker
	return f
}

func (f *workflowTestFixture) cleanup() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func (f *workflowTestFixture) executeWorkflow(workflowName string, input any) map[string]any {
	f.t.Helper()

	opts := client.StartWorkflowOptions{
		ID:        workflowName + "-" + time.Now().Format("20060102-150405.000"),
		TaskQueue: f.taskQueue,
	}

	we, err := f.temporalClient.ExecuteWorkflow(f.ctx, opts, workflowName, input)
	require.NoError(f.t, err)

	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()

	var result map[string]any
	err = we.Get(ctx, &result)
	require.NoError(f.t, err)
	return result
}

func (f *workflowTestFixture) startWorkflow(workflowName string, input any) client.WorkflowRun {
	f.t.Helper()

	opts := client.StartWorkflowOptions{
		ID:        workflowName + "-" + time.Now().Format("20060102-150405.000"),
		TaskQueue: f.taskQueue,
	}

	we, err := f.temporalClient.ExecuteWorkflow(f.ctx, opts, workflowName, input)
	require.NoError(f.t, err)
	return we
}

func newTestTranscoder() payload.Transcoder {
	transcoder := syspayload.NewTranscoder()
	enginepayload.RegisterAllBasicFormats(transcoder)
	msgpayload.Register(transcoder)
	return transcoder
}

type testResourceRegistry struct {
	resources map[registry.ID]any
}

func newTestResourceRegistry() *testResourceRegistry {
	return &testResourceRegistry{resources: make(map[registry.ID]any)}
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

// requireNumericEqual asserts that a JSON-decoded numeric value matches the expected float64.
// JSON roundtrip through Temporal's data converter produces float64 for all numbers.
func requireNumericEqual(t *testing.T, expected float64, actual any, msgAndArgs ...any) {
	t.Helper()
	v, ok := actual.(float64)
	require.True(t, ok, "expected float64, got %T", actual)
	require.Equal(t, expected, v, msgAndArgs...)
}

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

func TestWorkflowExecution_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "hello")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		workflowID: wfID,
		source:     helloWorkflowSource,
		taskQueue:  "test-workflow-queue",
	})
	defer f.cleanup()

	result := f.executeWorkflow(wfID.String(), map[string]any{"name": "Temporal"})
	require.Equal(t, "Hello, Temporal!", result["message"])
	require.Equal(t, "completed", result["status"])
}

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

func TestConcurrentWorkflow_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "concurrent")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:    []*luaapi.ModuleDef{timemod.Module},
		workflowID: wfID,
		source:     concurrentWorkflowSource,
		taskQueue:  "test-concurrent-queue",
		imports:    []code.Import{{ID: registry.NewID("", "time"), Alias: "time"}},
	})
	defer f.cleanup()

	result := f.executeWorkflow(wfID.String(), map[string]any{"workers": 3, "jobs": 6})

	// sum of (1+2+3+4+5+6)*2 = 42
	requireNumericEqual(t, 42, result["total"])
	requireNumericEqual(t, 6, result["job_count"])
	requireNumericEqual(t, 3, result["worker_count"])
}

const cancellableWorkflowSource = `
local time = require("time")
local channel = require("channel")
local process = require("process")

local function main(input)
    local timeout_ms = input and input.timeout or 10000

    local events_ch = process.events()
    if not events_ch then
        return { status = "no_events_channel" }
    end

    local timeout_ch = time.after(timeout_ms * time.MILLISECOND)

    local result = channel.select{
        events_ch:case_receive(),
        timeout_ch:case_receive()
    }

    if result.channel == events_ch then
        local event = result.value
        if event and event.kind == process.event.CANCEL then
            return { status = "canceled" }
        end
        return { status = "received_event", kind = event and event.kind }
    elseif result.channel == timeout_ch then
        return { status = "timeout" }
    end

    return { status = "unknown" }
end

return main
`

var processWorkflowClasses = []string{
	luaapi.ClassDeterministic, luaapi.ClassWorkflow, luaapi.ClassProcess, luaapi.ClassTime,
}

var processWorkflowImports = []code.Import{
	{ID: registry.NewID("", "time"), Alias: "time"},
	{ID: registry.NewID("", "process"), Alias: "process"},
}

func TestWorkflowCancellation_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "cancellable")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:        []*luaapi.ModuleDef{timemod.Module, processmod.Module},
		allowedClasses: processWorkflowClasses,
		engineOpts:     []engine.FactoryOption{engine.WithModule(processmod.Module)},
		workflowID:     wfID,
		source:         cancellableWorkflowSource,
		taskQueue:      "test-cancel-queue",
		imports:        processWorkflowImports,
	})
	defer f.cleanup()

	we := f.startWorkflow(wfID.String(), map[string]any{"timeout": 10000})

	time.Sleep(500 * time.Millisecond)

	err := f.temporalClient.CancelWorkflow(f.ctx, we.GetID(), we.GetRunID())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()

	var result map[string]any
	err = we.Get(ctx, &result)
	require.NoError(t, err)
	require.Equal(t, "canceled", result["status"])
}

const signalReceiverWorkflowSource = `
local time = require("time")
local channel = require("channel")
local process = require("process")

local function main(input)
    local my_pid = process.pid()
    local timeout_ms = input and input.timeout or 5000

    local greeting_ch, err = process.listen("greeting", {message = true})
    if err then
        return { pid = my_pid, status = "listen_error", error = tostring(err) }
    end

    local timeout_ch, err2 = time.after(timeout_ms * time.MILLISECOND)
    if err2 then
        return { pid = my_pid, status = "timer_error", error = tostring(err2) }
    end

    local result = channel.select{
        greeting_ch:case_receive(),
        timeout_ch:case_receive()
    }

    if result.channel == timeout_ch then
        return { pid = my_pid, status = "timeout" }
    end

    local msg = result.value
    return {
        pid = my_pid,
        received_topic = msg:topic(),
        status = "received"
    }
end

return main
`

func TestWorkflowSignal_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "signal_receiver")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:        []*luaapi.ModuleDef{timemod.Module, processmod.Module},
		allowedClasses: processWorkflowClasses,
		engineOpts:     []engine.FactoryOption{engine.WithModule(processmod.Module)},
		workflowID:     wfID,
		source:         signalReceiverWorkflowSource,
		taskQueue:      "test-signal-queue",
		imports:        processWorkflowImports,
	})
	defer f.cleanup()

	we := f.startWorkflow(wfID.String(), map[string]any{"timeout": 10000})

	time.Sleep(200 * time.Millisecond)

	err := f.temporalClient.SignalWorkflow(f.ctx, we.GetID(), we.GetRunID(), "greeting", map[string]any{
		"text": "hello from Go",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()

	var result map[string]any
	err = we.Get(ctx, &result)
	require.NoError(t, err)
	require.Equal(t, "received", result["status"])
	require.Equal(t, "greeting", result["received_topic"])
}

const tickerWorkflowSource = `
local time = require("time")
local channel = require("channel")
local process = require("process")

local function main(input)
    local count = input and input.count or 3
    local interval_ms = input and input.interval or 100

    local ticker = time.ticker(interval_ms * time.MILLISECOND)
    local tick_ch = ticker:channel()
    local ticks = {}

    for i = 1, count do
        local result = channel.select{
            tick_ch:case_receive()
        }
        table.insert(ticks, i)
    end

    ticker:stop()

    return {
        tick_count = #ticks,
        status = "completed"
    }
end

return main
`

func TestWorkflowTicker_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "ticker_test")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:        []*luaapi.ModuleDef{timemod.Module, processmod.Module},
		allowedClasses: processWorkflowClasses,
		engineOpts:     []engine.FactoryOption{engine.WithModule(processmod.Module)},
		workflowID:     wfID,
		source:         tickerWorkflowSource,
		taskQueue:      "test-ticker-queue",
		imports:        processWorkflowImports,
	})
	defer f.cleanup()

	result := f.executeWorkflow(wfID.String(), map[string]any{"count": 3, "interval": 100})
	require.Equal(t, "completed", result["status"])
	requireNumericEqual(t, 3, result["tick_count"])
}

const cryptoSideEffectWorkflowSource = `
local crypto = require("crypto")

local function main(input)
    local bytes, err = crypto.random.bytes(16)
    if err then
        return {status = "error", error = tostring(err)}
    end

    local id, err = crypto.random.uuid()
    if err then
        return {status = "error", error = tostring(err)}
    end

    local ciphertext, err = crypto.encrypt.aes(input.message or "hello", "0123456789abcdef")
    if err then
        return {status = "error", error = tostring(err)}
    end

    return {
        status = "completed",
        bytes_len = #bytes,
        uuid_len = #id,
        ciphertext_len = #ciphertext
    }
end

return main
`

func TestWorkflowCryptoSideEffect_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "crypto_side_effect")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:        []*luaapi.ModuleDef{cryptomod.Module},
		allowedClasses: []string{luaapi.ClassDeterministic, luaapi.ClassWorkflow, luaapi.ClassNondeterministic, luaapi.ClassSecurity},
		workflowID:     wfID,
		source:         cryptoSideEffectWorkflowSource,
		taskQueue:      "test-crypto-side-effect-queue",
		imports:        []code.Import{{ID: registry.NewID("", "crypto"), Alias: "crypto"}},
	})
	defer f.cleanup()

	result := f.executeWorkflow(wfID.String(), map[string]any{"message": "Temporal crypto"})
	require.Equal(t, "completed", result["status"])
	requireNumericEqual(t, 16, result["bytes_len"])
	requireNumericEqual(t, 36, result["uuid_len"])
	require.True(t, result["ciphertext_len"].(float64) > 0)
}

const uuidSideEffectWorkflowSource = `
local uuid = require("uuid")

local function main()
    local id, err = uuid.v4()
    if err then
        return {status = "error", error = tostring(err)}
    end

    local version, verr = uuid.version(id)
    if verr then
        return {status = "error", error = tostring(verr)}
    end

    local valid = uuid.validate(id)
    return {
        status = "completed",
        id = id,
        valid = valid,
        version = version
    }
end

return main
`

func TestWorkflowUUIDSideEffect_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "uuid_side_effect")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		modules:        []*luaapi.ModuleDef{uuidmod.Module},
		allowedClasses: []string{luaapi.ClassDeterministic, luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
		workflowID:     wfID,
		source:         uuidSideEffectWorkflowSource,
		taskQueue:      "test-uuid-side-effect-queue",
		imports:        []code.Import{{ID: registry.NewID("", "uuid"), Alias: "uuid"}},
	})
	defer f.cleanup()

	result := f.executeWorkflow(wfID.String(), nil)
	require.Equal(t, "completed", result["status"])
	require.Equal(t, true, result["valid"])
	requireNumericEqual(t, 4, result["version"])
}
