package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

// testTemporalClient embeds client.Client and overrides only the methods Worker uses.
type testTemporalClient struct {
	client.Client
	executeWorkflowFn         func(ctx context.Context, opts client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
	signalWorkflowFn          func(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
	signalWithStartWorkflowFn func(ctx context.Context, workflowID string, signalName string, signalArg interface{}, options client.StartWorkflowOptions, workflow interface{}, workflowArgs ...interface{}) (client.WorkflowRun, error)
	terminateFn               func(ctx context.Context, workflowID string, runID string, reason string, details ...interface{}) error
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

func (t *testTemporalClient) SignalWithStartWorkflow(
	ctx context.Context,
	workflowID string,
	signalName string,
	signalArg interface{},
	options client.StartWorkflowOptions,
	workflow interface{},
	workflowArgs ...interface{},
) (client.WorkflowRun, error) {
	if t.signalWithStartWorkflowFn != nil {
		return t.signalWithStartWorkflowFn(ctx, workflowID, signalName, signalArg, options, workflow, workflowArgs...)
	}
	return &testWorkflowRun{}, nil
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

type testRunHandoff struct {
	clientID   string
	workflowID string
	runID      string
	published  bool
}

func (h *testRunHandoff) Publish(clientID, workflowID, runID string) {
	h.clientID = clientID
	h.workflowID = workflowID
	h.runID = runID
	h.published = true
}

func (h *testRunHandoff) Consume(_, _ string) (string, bool) {
	return "", false
}

func newHostTestWorker(tc client.Client) *Worker {
	cfg := &temporalapi.WorkerConfig{
		Client:    registry.ID{NS: "test", Name: "client"},
		TaskQueue: "test-queue",
	}
	w, _ := NewWorkerBuilder().
		WithID(registry.ID{NS: "test", Name: "worker"}).
		WithConfig(cfg).
		WithTranscoder(newWorkerTestTranscoder()).
		Build()
	w.temporalClient = tc
	w.workflowPrefix = "test-wf"
	return w
}

func namedOptions(name string) attrs.Bag {
	options := attrs.NewBag()
	options.Set(process.ProcessNameKey, name)
	return options
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
	cfg := &temporalapi.WorkerConfig{
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
	var sawPolicy bool
	var sawConflictPolicy enumspb.WorkflowIdConflictPolicy
	w := newHostTestWorker(&testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			sawPolicy = opts.WorkflowExecutionErrorWhenAlreadyStarted
			sawConflictPolicy = opts.WorkflowIDConflictPolicy
			return &testWorkflowRun{}, nil
		},
	})

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: namedOptions("my-workflow-id"),
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "my-workflow-id", resultPID.UniqID)
	assert.False(t, sawPolicy)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, sawConflictPolicy)
}

func TestWorker_Run_WithStartOptionsWorkflowID(t *testing.T) {
	var gotID string
	var gotPolicy bool
	var gotConflictPolicy enumspb.WorkflowIdConflictPolicy
	w := newHostTestWorker(&testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			gotID = opts.ID
			gotPolicy = opts.WorkflowExecutionErrorWhenAlreadyStarted
			gotConflictPolicy = opts.WorkflowIDConflictPolicy
			return &testWorkflowRun{}, nil
		},
	})

	options := attrs.NewBag()
	options.Set(optionWorkflowID, "workflow-from-options")

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "workflow-from-options", gotID)
	assert.Equal(t, "workflow-from-options", resultPID.UniqID)
	assert.False(t, gotPolicy)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, gotConflictPolicy)
}

func TestWorker_Run_WithStartOptionsTaskQueueOverride(t *testing.T) {
	var gotTaskQueue string
	w := newHostTestWorker(&testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			gotTaskQueue = opts.TaskQueue
			return &testWorkflowRun{}, nil
		},
	})

	options := attrs.NewBag()
	options.Set(optionWorkflowTaskQueue, "custom-task-queue")

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "custom-task-queue", gotTaskQueue)
}

func TestWorker_Run_AlreadyStarted_WithOptionWorkflowID(t *testing.T) {
	calls := 0
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			calls++
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
		},
	}
	w := newHostTestWorker(tc)

	options := attrs.NewBag()
	options.Set(optionWorkflowID, "orders:123")

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "orders:123", resultPID.UniqID)
	assert.Equal(t, 1, calls)
}

func TestWorker_Run_AlreadyStarted_WithName(t *testing.T) {
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: namedOptions("existing-workflow"),
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "existing-workflow", resultPID.UniqID)
}

func TestWorker_Run_AlreadyStarted_WithoutName_ReturnsError(t *testing.T) {
	var ids []string
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			ids = append(ids, opts.ID)
			assert.True(t, opts.WorkflowExecutionErrorWhenAlreadyStarted)
			assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL, opts.WorkflowIDConflictPolicy)
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "")
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source: registry.NewID("app", "my-workflow"),
	}

	_, err := w.Run(context.Background(), start)
	require.Error(t, err)
	var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
	require.ErrorAs(t, err, &alreadyStarted)
	require.Len(t, ids, 1)
	assert.Equal(t, "test-wf_0x00001", ids[0])
}

func TestWorker_Run_AlreadyStarted_WithoutName_UseExisting(t *testing.T) {
	var ids []string
	calls := 0
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			calls++
			ids = append(ids, opts.ID)
			assert.False(t, opts.WorkflowExecutionErrorWhenAlreadyStarted)
			assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, opts.WorkflowIDConflictPolicy)
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "existing-run")
		},
	}
	w := newHostTestWorker(tc)

	options := attrs.NewBag()
	options.Set(optionWorkflowExecutionErrorWhenAlreadyStarted, false)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "test-wf_0x00001", resultPID.UniqID)
	assert.Equal(t, 1, calls)
	require.Len(t, ids, 1)
	assert.Equal(t, "test-wf_0x00001", ids[0])
}

func TestWorker_Run_AlreadyStartedPolicy_OverrideFromStartOptions(t *testing.T) {
	var policies []bool
	var conflictPolicies []enumspb.WorkflowIdConflictPolicy
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, opts client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			policies = append(policies, opts.WorkflowExecutionErrorWhenAlreadyStarted)
			conflictPolicies = append(conflictPolicies, opts.WorkflowIDConflictPolicy)
			return &testWorkflowRun{}, nil
		},
	}
	w := newHostTestWorker(tc)

	options := attrs.NewBag()
	options.Set(optionWorkflowExecutionErrorWhenAlreadyStarted, false)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.False(t, policies[0])
	require.Len(t, conflictPolicies, 1)
	assert.Equal(t, enumspb.WORKFLOW_ID_CONFLICT_POLICY_USE_EXISTING, conflictPolicies[0])
}

func TestWorker_Run_InvalidStartOptionType(t *testing.T) {
	w := newHostTestWorker(&testTemporalClient{})

	options := attrs.NewBag()
	options.Set(optionWorkflowTaskQueue, true)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
	}

	_, err := w.Run(context.Background(), start)
	require.Error(t, err)
	assert.Contains(t, err.Error(), optionWorkflowTaskQueue+" must be a string")
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
	var signalWithStartCalls int
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			require.Fail(t, "ExecuteWorkflow should not be called when startup messages exist")
			return nil, nil
		},
		signalWithStartWorkflowFn: func(_ context.Context, workflowID string, signalName string, signalArg interface{}, opts client.StartWorkflowOptions, workflow interface{}, workflowArgs ...interface{}) (client.WorkflowRun, error) {
			signalWithStartCalls++
			assert.Equal(t, "init", signalName)
			assert.Equal(t, "test-wf_0x00001", workflowID)
			assert.Equal(t, "test-wf_0x00001", opts.ID)
			assert.Equal(t, "app:my-workflow", workflow)
			require.Len(t, workflowArgs, 1)
			return &testWorkflowRun{}, nil
		},
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
	assert.Equal(t, 1, signalWithStartCalls)
	assert.Equal(t, 1, signalCalls)
}

func TestWorker_Run_WithMessages_AlreadyStarted_UseExisting(t *testing.T) {
	var signalCalls int
	tc := &testTemporalClient{
		signalWithStartWorkflowFn: func(_ context.Context, _ string, _ string, _ interface{}, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "existing-run")
		},
		signalWorkflowFn: func(_ context.Context, workflowID string, _ string, _ string, _ interface{}) error {
			signalCalls++
			assert.Equal(t, "existing-workflow", workflowID)
			return nil
		},
	}
	w := newHostTestWorker(tc)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: namedOptions("existing-workflow"),
		Messages: []*relay.Message{
			{Topic: "init"},
			{Topic: "config"},
		},
	}

	resultPID, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.Equal(t, "existing-workflow", resultPID.UniqID)
	assert.Equal(t, 2, signalCalls)
}

func TestWorker_Run_WithMessages_AlreadyStarted_FailPolicy(t *testing.T) {
	var signalCalls int
	tc := &testTemporalClient{
		signalWithStartWorkflowFn: func(_ context.Context, _ string, _ string, _ interface{}, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "existing-run")
		},
		signalWorkflowFn: func(_ context.Context, _ string, _ string, _ string, _ interface{}) error {
			signalCalls++
			return nil
		},
	}
	w := newHostTestWorker(tc)

	options := namedOptions("existing-workflow")
	options.Set(optionWorkflowIDConflictPolicy, enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL)

	start := &process.Start{
		Source:  registry.NewID("app", "my-workflow"),
		Options: options,
		Messages: []*relay.Message{
			{Topic: "init"},
		},
	}

	_, err := w.Run(context.Background(), start)
	require.Error(t, err)
	assert.Equal(t, 0, signalCalls)
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

func TestWorker_Run_PublishesRunHandoffForLifecycleWatch(t *testing.T) {
	handoff := &testRunHandoff{}
	tc := &testTemporalClient{
		executeWorkflowFn: func(_ context.Context, _ client.StartWorkflowOptions, _ interface{}, _ ...interface{}) (client.WorkflowRun, error) {
			return &testWorkflowRun{}, nil
		},
	}
	w := newHostTestWorker(tc)
	w.ctx = temporalapi.WithWorkflowRunHandoff(context.Background(), handoff)

	options := attrs.NewBag()
	options.Set(process.ProcessMonitorKey, true)
	start := &process.Start{
		Source:  registry.NewID("app", "wf"),
		Options: options,
	}

	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)

	require.True(t, handoff.published)
	assert.Equal(t, w.config.Client.String(), handoff.clientID)
	assert.Equal(t, "run-id", handoff.runID)
}

func TestWorker_Run_DoesNotPublishRunHandoffWithoutLifecycleWatch(t *testing.T) {
	handoff := &testRunHandoff{}
	w := newHostTestWorker(&testTemporalClient{})
	w.ctx = temporalapi.WithWorkflowRunHandoff(context.Background(), handoff)

	start := &process.Start{
		Source: registry.NewID("app", "wf"),
	}

	_, err := w.Run(context.Background(), start)
	require.NoError(t, err)
	assert.False(t, handoff.published)
}

// --- Terminate ---

func TestWorker_Terminate_NoClient(t *testing.T) {
	cfg := &temporalapi.WorkerConfig{TaskQueue: "q"}
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
