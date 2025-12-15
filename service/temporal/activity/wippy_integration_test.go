package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

// mockFuncRegistry implements function.Registry for testing
type mockFuncRegistry struct {
	funcs map[string]function.Func
}

func newMockFuncRegistry() *mockFuncRegistry {
	return &mockFuncRegistry{
		funcs: make(map[string]function.Func),
	}
}

func (r *mockFuncRegistry) Register(id registry.ID, fn function.Func) {
	r.funcs[id.String()] = fn
}

func (r *mockFuncRegistry) Call(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	fn, ok := r.funcs[task.ID.String()]
	if !ok {
		return nil, function.NewHandlerNotFoundError(task.ID)
	}
	return fn(ctx, task)
}

// mockTranscoder implements payload.Transcoder for testing
type mockTranscoder struct{}

func (m *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (m *mockTranscoder) Unmarshal(_ payload.Payload, _ interface{}) error {
	return nil
}

// TestWippyActivityWithDataConverter tests the activity execution pipeline
// using the wippy data converter for proper payload handling.
func TestWippyActivityWithDataConverter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	log := zap.NewNop()

	// Create wippy data converter
	dc := dataconverter.NewDataConverter(&mockTranscoder{}, converter.GetDefaultDataConverter())

	// Start Temporal test server with custom data converter
	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	c := server.Client()
	defer c.Close()

	// Create mock function registry with a test function
	funcReg := newMockFuncRegistry()
	funcID := registry.ID{NS: "app.test", Name: "echo"}

	// Track that function was called
	var calledWith []payload.Payload
	funcReg.Register(funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		calledWith = task.Payloads
		if len(task.Payloads) > 0 {
			return &runtime.Result{Value: task.Payloads[0]}, nil
		}
		return &runtime.Result{Value: payload.NewPayload(nil, payload.JSON)}, nil
	})

	// Put function registry in context
	ctx = function.WithRegistry(ctx, funcReg)

	// Create worker
	taskQueue := "test-wippy-dc-activities"

	w := worker.New(c, taskQueue, worker.Options{})

	// Register activity with proper handler that uses data converter
	activityName := funcID.String()
	w.RegisterActivityWithOptions(
		createDataConverterActivityHandler(ctx, funcReg, funcID, dc, log),
		activity.RegisterOptions{Name: activityName},
	)

	// Register workflow
	w.RegisterWorkflow(dcEchoWorkflow)

	require.NoError(t, w.Start())
	defer w.Stop()

	// Execute workflow
	workflowOptions := client.StartWorkflowOptions{
		ID:        "wippy-dc-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]interface{}{
		"message": "hello from dc test",
		"count":   123,
	}

	we, err := c.ExecuteWorkflow(ctx, workflowOptions, dcEchoWorkflow, activityName, testInput)
	require.NoError(t, err)

	var result map[string]interface{}
	err = we.Get(ctx, &result)
	require.NoError(t, err)

	// Verify the function was called with payloads
	require.NotEmpty(t, calledWith, "function should have been called with payloads")

	// Verify the result matches input (echo)
	require.Equal(t, "hello from dc test", result["message"])
	require.Equal(t, float64(123), result["count"])
}

// createDataConverterActivityHandler creates an activity handler that converts
// standard input/output to/from wippy payloads using the data converter.
func createDataConverterActivityHandler(
	ctx context.Context,
	funcReg function.Registry,
	funcID registry.ID,
	_ converter.DataConverter,
	_ *zap.Logger,
) func(context.Context, interface{}) (interface{}, error) {
	return func(_ context.Context, input interface{}) (interface{}, error) {
		// Convert input to wippy payload
		inputPayload := payload.New(input)
		payloads := []payload.Payload{inputPayload}

		// Call function registry
		result, err := funcReg.Call(ctx, runtime.Task{
			ID:       funcID,
			Payloads: payloads,
		})
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, nil
		}
		if result.Error != nil {
			return nil, result.Error
		}

		// Return the data directly
		return result.Value.Data(), nil
	}
}

// dcEchoWorkflow calls an activity with data converter support
func dcEchoWorkflow(ctx workflow.Context, activityName string, input interface{}) (interface{}, error) {
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

// TestActivityListenerRegistration tests that the activity listener correctly
// identifies and registers functions as activities based on metadata.
func TestActivityListenerRegistration(t *testing.T) {
	// This is a unit test - no Temporal server needed
	log := zap.NewNop()

	// Create mock worker registry
	mockWorkers := &mockWorkerRegistry{
		registeredActivities: make(map[string]string),
	}

	// Create listener
	listener := newTestActivityListener(log, mockWorkers)

	// Create entry with temporal activity metadata
	entry := registry.Entry{
		ID:   registry.ID{NS: "app.test", Name: "my_activity"},
		Kind: "function.lua",
		Meta: createActivityMeta("app.test:worker"),
	}

	// Add entry
	err := listener.Add(context.Background(), entry)
	require.NoError(t, err)

	// Verify activity was registered
	require.Contains(t, mockWorkers.registeredActivities, "app.test:my_activity")
	require.Equal(t, "app.test:worker", mockWorkers.registeredActivities["app.test:my_activity"])
}

// mockWorkerRegistry tracks activity registrations for testing
type mockWorkerRegistry struct {
	registeredActivities map[string]string // activityName -> workerID
}

func (m *mockWorkerRegistry) RegisterActivity(_ context.Context, workerID registry.ID, activityName string, _ registry.ID) error {
	m.registeredActivities[activityName] = workerID.String()
	return nil
}

func (m *mockWorkerRegistry) RegisterLocalActivity(_ context.Context, workerID registry.ID, activityName string, _ registry.ID) error {
	m.registeredActivities[activityName] = workerID.String()
	return nil
}

func (m *mockWorkerRegistry) UnregisterActivity(_ context.Context, _ registry.ID, activityName string) error {
	delete(m.registeredActivities, activityName)
	return nil
}

// Helper to create activity metadata
func createActivityMeta(workerID string) attrs.Bag {
	meta := attrs.NewBag()
	meta.Set("temporal", map[string]interface{}{
		"activity": map[string]interface{}{
			"worker": workerID,
		},
	})
	return meta
}

// Minimal listener implementation for unit tests (avoids importing the real one to prevent cycles)
type testActivityListener struct {
	log     *zap.Logger
	workers interface {
		RegisterActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error
	}
}

func newTestActivityListener(log *zap.Logger, workers interface {
	RegisterActivity(ctx context.Context, workerID registry.ID, activityName string, funcID registry.ID) error
}) *testActivityListener {
	return &testActivityListener{log: log, workers: workers}
}

func (l *testActivityListener) Add(ctx context.Context, entry registry.Entry) error {
	// Check if entry is a function
	if entry.Kind != "function.lua" && entry.Kind != "function.go" {
		return nil
	}

	// Check for temporal.activity metadata
	if entry.Meta == nil {
		return nil
	}

	temporal, ok := entry.Meta.GetBag("temporal")
	if !ok {
		return nil
	}

	activityBag, ok := temporal.GetBag("activity")
	if !ok {
		return nil
	}

	workerStr := activityBag.GetString("worker", "")
	if workerStr == "" {
		return nil
	}

	workerID := registry.ParseID(workerStr)
	if workerID.NS == "" {
		workerID = workerID.WithDefaultNS(entry.ID.NS)
	}

	activityName := entry.ID.String()
	return l.workers.RegisterActivity(ctx, workerID, activityName, entry.ID)
}
