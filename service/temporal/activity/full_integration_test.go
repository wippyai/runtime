package activity_test

import (
	"context"
	"sync"
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
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/worker"
	"github.com/wippyai/runtime/system/eventbus"
	sysfunction "github.com/wippyai/runtime/system/function"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

// fullTestTranscoder implements payload.Transcoder for testing
type fullTestTranscoder struct{}

func (m *fullTestTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *fullTestTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return nil
}

// mockResourceRegistry provides a mock resource registry for testing
type mockResourceRegistry struct {
	resources map[registry.ID]any
}

func newMockResourceRegistry() *mockResourceRegistry {
	return &mockResourceRegistry{
		resources: make(map[registry.ID]any),
	}
}

func (m *mockResourceRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	val, ok := m.resources[id]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &mockResource{value: val}, nil
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	var ids []registry.ID
	for id := range m.resources {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.resources[id]
	return ok
}

type mockResource struct {
	value any
}

func (r *mockResource) Get() (any, error) { return r.value, nil }
func (r *mockResource) Release()          {}

// TestFullStackActivityExecution tests the complete activity execution path:
// Temporal dev server -> wippy worker -> function registry -> native Go function
func TestFullStackActivityExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zap.NewNop()
	bus := eventbus.NewBus()

	// Set up function registry
	funcRegistry := sysfunction.NewFunctionRegistry(bus, logger)

	// Set up root context with all dependencies
	ctx := ctxapi.NewRootContext()
	ctx = function.WithRegistry(ctx, funcRegistry)

	// Set up PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test")
	ctx = process.WithPIDGenerator(ctx, pidGen)

	require.NoError(t, funcRegistry.Start(ctx))
	defer func() { _ = funcRegistry.Stop() }()

	// Register test function that will be called as activity
	funcID := registry.NewID("test.activity", "echo_data")
	var activityCalled bool
	var receivedPayloads []payload.Payload

	registerFunction(t, ctx, bus, funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		activityCalled = true
		receivedPayloads = task.Payloads

		// Echo back the first payload if available
		if len(task.Payloads) > 0 {
			return &runtime.Result{Value: task.Payloads[0]}, nil
		}
		return &runtime.Result{Value: payload.New(nil)}, nil
	})

	// Create data converter
	dc := dataconverter.NewDataConverter(&fullTestTranscoder{}, converter.GetDefaultDataConverter())

	// Start Temporal test server with custom data converter
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	temporalClient := server.Client()
	defer temporalClient.Close()

	// Create mock resource registry with the client
	resourceReg := newMockResourceRegistry()
	clientResource := api.ClientResource{
		Client: temporalClient,
	}
	clientID := registry.NewID("test", "client")
	resourceReg.resources[clientID] = clientResource

	// Create wippy worker
	taskQueue := "test-full-stack-queue"
	workerConfig := &api.WorkerConfig{
		Client:    clientID,
		TaskQueue: taskQueue,
		WorkerOptions: api.WorkerOptionsConfig{
			MaxConcurrentActivityExecutionSize: 10,
		},
	}

	wippyWorker := worker.NewWorker(
		logger,
		registry.NewID("test", "worker"),
		workerConfig,
		resourceReg,
		nil,
	)

	// Register activity before starting worker
	activityName := funcID.String()
	require.NoError(t, wippyWorker.RegisterActivity(ctx, activityName, funcID))

	// Start the worker
	statusCh, err := wippyWorker.Start(ctx)
	require.NoError(t, err)

	// Wait for worker to be running
	status := <-statusCh
	require.NotNil(t, status)

	defer func() { _ = wippyWorker.Stop(ctx) }()

	// Create a separate SDK worker just for workflow registration
	// (wippy worker handles activities, this handles workflow)
	sdkWorker := sdkworker.New(temporalClient, taskQueue, sdkworker.Options{})
	sdkWorker.RegisterWorkflow(testFullStackWorkflow)
	require.NoError(t, sdkWorker.Start())
	defer sdkWorker.Stop()

	// Execute workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "full-stack-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]interface{}{
		"message": "hello full stack",
		"number":  42,
	}

	we, err := temporalClient.ExecuteWorkflow(ctx, workflowOptions, testFullStackWorkflow, activityName, testInput)
	require.NoError(t, err)

	var result map[string]interface{}
	err = we.Get(ctx, &result)
	require.NoError(t, err)

	// Verify activity was called
	require.True(t, activityCalled, "activity should have been called")
	require.NotEmpty(t, receivedPayloads, "should have received payloads")

	// Verify result
	require.Equal(t, "hello full stack", result["message"])
	require.Equal(t, float64(42), result["number"])
}

// testFullStackWorkflow is a Go workflow that calls a wippy activity
func testFullStackWorkflow(ctx workflow.Context, activityName string, input interface{}) (interface{}, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result interface{}
	err := workflow.ExecuteActivity(ctx, activityName, input).Get(ctx, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// registerFunction registers a function with the function registry via event bus
func registerFunction(t *testing.T, ctx context.Context, bus event.Bus, funcID registry.ID, handler function.Func) {
	var wg sync.WaitGroup
	sub, err := eventbus.NewSubscriber(ctx, bus, function.System, "function.*", func(evt event.Event) {
		if evt.Kind == function.Accept && evt.Path == funcID.String() {
			wg.Done()
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	wg.Add(1)
	bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   funcID.String(),
		Data: &function.FuncEntry{
			Handler: handler,
			Options: nil,
		},
	})
	wg.Wait()
}
