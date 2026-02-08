package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	sysfunc "github.com/wippyai/runtime/system/function"
	syspayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	msgpayload "github.com/wippyai/runtime/system/payload/msgpack"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

func newTestTranscoder() payload.Transcoder {
	transcoder := syspayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	msgpayload.Register(transcoder)
	return transcoder
}

// mockFuncRegistry implements function.Registry for testing.
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
		return nil, sysfunc.NewHandlerNotFoundError(task.ID)
	}
	return fn(ctx, task)
}

// TestWippyActivityWithDataConverter tests the activity execution pipeline
// using the wippy data converter for proper payload handling.
func TestWippyActivityWithDataConverter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	log := zap.NewNop()

	dc := dataconverter.NewDataConverter(newTestTranscoder())

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
		LogLevel:      "error",
		ClientOptions: &client.Options{DataConverter: dc},
	})
	require.NoError(t, err)
	defer func() { _ = server.Stop() }()

	c := server.Client()
	defer c.Close()

	funcReg := newMockFuncRegistry()
	funcID := registry.ID{NS: "app.test", Name: "echo"}

	var calledWith []payload.Payload
	funcReg.Register(funcID, func(_ context.Context, task runtime.Task) (*runtime.Result, error) {
		calledWith = task.Payloads
		if len(task.Payloads) > 0 {
			return &runtime.Result{Value: task.Payloads[0]}, nil
		}
		return &runtime.Result{Value: payload.NewPayload(nil, payload.JSON)}, nil
	})

	ctx = function.WithRegistry(ctx, funcReg)

	taskQueue := "test-wippy-dc-activities"
	w := worker.New(c, taskQueue, worker.Options{})

	activityName := funcID.String()
	w.RegisterActivityWithOptions(
		createDataConverterActivityHandler(ctx, funcReg, funcID, log),
		activity.RegisterOptions{Name: activityName},
	)
	w.RegisterWorkflow(dcEchoWorkflow)

	require.NoError(t, w.Start())
	defer w.Stop()

	workflowOptions := client.StartWorkflowOptions{
		ID:        "wippy-dc-test-" + time.Now().Format("20060102-150405"),
		TaskQueue: taskQueue,
	}

	testInput := map[string]any{
		"message": "hello from dc test",
		"count":   123,
	}

	we, err := c.ExecuteWorkflow(ctx, workflowOptions, dcEchoWorkflow, activityName, testInput)
	require.NoError(t, err)

	var result map[string]any
	err = we.Get(ctx, &result)
	require.NoError(t, err)

	require.NotEmpty(t, calledWith, "function should have been called with payloads")
	require.Equal(t, "hello from dc test", result["message"])
	require.Equal(t, float64(123), result["count"])
}

// createDataConverterActivityHandler creates an activity handler that converts
// standard input/output to/from wippy payloads using the data converter.
func createDataConverterActivityHandler(
	ctx context.Context,
	funcReg function.Registry,
	funcID registry.ID,
	_ *zap.Logger,
) func(context.Context, any) (any, error) {
	return func(_ context.Context, input any) (any, error) {
		inputPayload := payload.New(input)
		payloads := []payload.Payload{inputPayload}

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

		return result.Value.Data(), nil
	}
}

// dcEchoWorkflow calls an activity with data converter support.
func dcEchoWorkflow(ctx workflow.Context, activityName string, input any) (any, error) {
	options := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, options)

	var result any
	err := workflow.ExecuteActivity(ctx, activityName, input).Get(ctx, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
