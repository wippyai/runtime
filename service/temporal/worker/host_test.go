package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	api "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// testTemporalClient embeds client.Client and overrides only the methods Worker uses.
type testTemporalClient struct {
	client.Client
	executeWorkflowFn func(ctx context.Context, opts client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
	signalWorkflowFn  func(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
	terminateFn       func(ctx context.Context, workflowID string, runID string, reason string, details ...interface{}) error
}

func (t *testTemporalClient) ExecuteWorkflow(ctx context.Context, opts client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error) {
	if t.executeWorkflowFn != nil {
		return t.executeWorkflowFn(ctx, opts, workflow, args...)
	}
	return &testWorkflowRun{}, nil
}

func (t *testTemporalClient) SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error {
	if t.signalWorkflowFn != nil {
		return t.signalWorkflowFn(ctx, workflowID, runID, signalName, arg)
	}
	return nil
}

func (t *testTemporalClient) TerminateWorkflow(ctx context.Context, workflowID string, runID string, reason string, details ...interface{}) error {
	if t.terminateFn != nil {
		return t.terminateFn(ctx, workflowID, runID, reason, details...)
	}
	return nil
}

type testWorkflowRun struct {
	client.WorkflowRun
}

func (t *testWorkflowRun) GetID() string    { return "wf-id" }
func (t *testWorkflowRun) GetRunID() string { return "run-id" }

func newHostTestWorker(tc client.Client) *Worker {
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	w, _ := NewWorkerBuilder().
		WithID(registry.ID{NS: "test", Name: "worker"}).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	w.temporalClient = tc
	return w
}

// --- Run ---

func TestWorker_Run_NilStart(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})
	_, err := w.Run(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start config is required")
}

func TestWorker_Run_Closed(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})
	w.closed.Store(true)
	_, err := w.Run(context.Background(), &process.Start{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestWorker_Run_NoClient(t *testing.T) {
	cfg := &api.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	w, _ := NewWorkerBuilder().
		WithID(registry.ID{NS: "test", Name: "worker"}).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()

	_, err := w.Run(context.Background(), &process.Start{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "temporal client not available")
}

func TestWorker_Run_Success(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.NotEmpty(t, resultPID.UniqID)
}

func TestWorker_Run_WithExplicitName(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
		Name:   "my-workflow-id",
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "my-workflow-id", resultPID.UniqID)
}

func TestWorker_Run_AlreadyStarted_WithName(t *testing.T) {
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
		Name:   "existing-workflow",
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "existing-workflow", resultPID.UniqID)
}

func TestWorker_Run_AlreadyStarted_WithoutName_ReturnsError(t *testing.T) {
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
	}

	_, err := w.Run(context.Background(), start)
	require.Error(t, err)
}

func TestWorker_Run_ExecuteError(t *testing.T) {
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
	}

	_, err := w.Run(context.Background(), start)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestWorker_Run_WithMessages(t *testing.T) {
	var signalCalls int
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, _ interface{}) error {
			signalCalls++
			return nil
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
		Messages: []*relay.Message{
			{Topic: "init"},
			{Topic: "config"},
		},
	}

	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, 2, signalCalls)
}

func TestWorker_Run_UsesConfigTaskQueue(t *testing.T) {
	var usedTaskQueue string
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			usedTaskQueue = opts.TaskQueue
			return &testWorkflowRun{}, nil
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{Source: registry.NewID("app", "wf")}
	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "test-queue", usedTaskQueue)
}

// --- Terminate ---

func TestWorker_Terminate_NoClient(t *testing.T) {
	cfg := &api.WorkerConfig{TaskQueue: "q"}
	w, _ := NewWorkerBuilder().
		WithID(registry.ID{Name: "w"}).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()

	err := w.Terminate(context.Background(), pid.PID{UniqID: "wf-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "temporal client not available")
}

func TestWorker_Terminate_EmptyTarget(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})
	err := w.Terminate(context.Background(), pid.PID{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestWorker_Terminate_Success(t *testing.T) {
	var terminatedID string
	tc := &testTemporalClient{
		terminateFn: func(_ context.Context, workflowID string, _ string, _ string, _ ...interface{}) error {
			terminatedID = workflowID
			return nil
		},
	}
	w := newHostTestWorker(tc)

	err := w.Terminate(context.Background(), pid.PID{UniqID: "wf-123"})
	require.NoError(t, err)
	assert.Equal(t, "wf-123", terminatedID)
}

func TestWorker_Terminate_Error(t *testing.T) {
	tc := &testTemporalClient{
		terminateFn: func(_ context.Context, _ string, _ string, _ string, _ ...interface{}) error {
			return fmt.Errorf("terminate failed")
		},
	}
	w := newHostTestWorker(tc)

	err := w.Terminate(context.Background(), pid.PID{UniqID: "wf-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminate failed")
}

// --- signalMessages ---

func TestWorker_SignalMessages_Empty(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})
	err := w.signalMessages(context.Background(), "wf-1", nil, nil)
	require.NoError(t, err)
}

func TestWorker_SignalMessages_SkipsNilAndEmptyTopic(t *testing.T) {
	var calls int
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, _ interface{}) error {
			calls++
			return nil
		},
	}
	w := newHostTestWorker(tc)

	msgs := []*relay.Message{
		nil,
		{Topic: ""},
		{Topic: "valid"},
	}
	err := w.signalMessages(context.Background(), "wf-1", msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestWorker_SignalMessages_Error(t *testing.T) {
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, _ interface{}) error {
			return fmt.Errorf("signal failed")
		},
	}
	w := newHostTestWorker(tc)

	msgs := []*relay.Message{{Topic: "test"}}
	err := w.signalMessages(context.Background(), "wf-1", msgs, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signal failed")
}

func TestWorker_SignalMessages_NoPayloads(t *testing.T) {
	var receivedArg interface{}
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, arg interface{}) error {
			receivedArg = arg
			return nil
		},
	}
	w := newHostTestWorker(tc)

	msgs := []*relay.Message{{Topic: "test"}}
	err := w.signalMessages(context.Background(), "wf-1", msgs, nil)
	require.NoError(t, err)
	assert.Nil(t, receivedArg)
}

func TestWorker_SignalMessages_SinglePayload(t *testing.T) {
	var receivedArg interface{}
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, arg interface{}) error {
			receivedArg = arg
			return nil
		},
	}
	w := newHostTestWorker(tc)

	p := payload.NewString("hello")
	msgs := []*relay.Message{{Topic: "test", Payloads: payload.Payloads{p}}}
	err := w.signalMessages(context.Background(), "wf-1", msgs, nil)
	require.NoError(t, err)
	assert.Equal(t, p, receivedArg)
}

func TestWorker_SignalMessages_MultiplePayloads(t *testing.T) {
	var receivedArg interface{}
	tc := &testTemporalClient{
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, arg interface{}) error {
			receivedArg = arg
			return nil
		},
	}
	w := newHostTestWorker(tc)

	p1 := payload.NewString("a")
	p2 := payload.NewString("b")
	msgs := []*relay.Message{{Topic: "test", Payloads: payload.Payloads{p1, p2}}}
	err := w.signalMessages(context.Background(), "wf-1", msgs, nil)
	require.NoError(t, err)
	arr, ok := receivedArg.(payload.Payloads)
	require.True(t, ok)
	assert.Len(t, arr, 2)
}
