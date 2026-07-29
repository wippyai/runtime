// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	clockapi "github.com/wippyai/runtime/api/clock"
	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	enginepayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/propagator"
	syspayload "github.com/wippyai/runtime/system/payload"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
)

type recordedTimer struct {
	callback bindings.ResultHandler
	duration time.Duration
}

type recordedSignal struct {
	arg               any
	input             *commonpb.Payloads
	header            *commonpb.Header
	callback          bindings.ResultHandler
	namespace         string
	workflowID        string
	runID             string
	topic             string
	childWorkflowOnly bool
}

type recordedEnvironment struct {
	bindings.WorkflowEnvironment
	dc           converter.DataConverter
	info         *workflow.Info
	timers       []recordedTimer
	activities   []bindings.ExecuteActivityParams
	activityDone []bindings.ResultHandler
	signals      []recordedSignal
	children     []bindings.ExecuteWorkflowParams
	childDone    []bindings.ResultHandler
	versions     []versionCall
	completions  []completionCall
	version      workflow.Version
}

type versionCall struct {
	changeID string
	min      workflow.Version
	max      workflow.Version
}

type completionCall struct {
	result *commonpb.Payloads
	err    error
}

func (e *recordedEnvironment) WorkflowInfo() *workflow.Info              { return e.info }
func (e *recordedEnvironment) GetDataConverter() converter.DataConverter { return e.dc }
func (e *recordedEnvironment) NewTimer(duration time.Duration, _ workflow.TimerOptions, callback bindings.ResultHandler) *bindings.TimerID {
	e.timers = append(e.timers, recordedTimer{duration: duration, callback: callback})
	return nil
}
func (e *recordedEnvironment) ExecuteActivity(params bindings.ExecuteActivityParams, callback bindings.ResultHandler) bindings.ActivityID {
	e.activities = append(e.activities, params)
	e.activityDone = append(e.activityDone, callback)
	return bindings.ActivityID{}
}
func (e *recordedEnvironment) SignalExternalWorkflow(namespace, workflowID, runID, topic string, input *commonpb.Payloads, arg any, header *commonpb.Header, childWorkflowOnly bool, callback bindings.ResultHandler) {
	e.signals = append(e.signals, recordedSignal{
		namespace: namespace, workflowID: workflowID, runID: runID, topic: topic,
		input: input, arg: arg, header: header, childWorkflowOnly: childWorkflowOnly, callback: callback,
	})
}
func (e *recordedEnvironment) ExecuteChildWorkflow(params bindings.ExecuteWorkflowParams, callback bindings.ResultHandler, _ func(bindings.WorkflowExecution, error)) {
	e.children = append(e.children, params)
	e.childDone = append(e.childDone, callback)
}
func (e *recordedEnvironment) GetVersion(changeID string, min, max workflow.Version) workflow.Version {
	e.versions = append(e.versions, versionCall{changeID: changeID, min: min, max: max})
	return e.version
}
func (e *recordedEnvironment) Complete(result *commonpb.Payloads, err error) {
	e.completions = append(e.completions, completionCall{result: result, err: err})
}

func (e *recordedEnvironment) actionCount() int {
	return len(e.timers) + len(e.activities) + len(e.signals) + len(e.children) + len(e.versions)
}

type recordingProcess struct {
	step func([]process.Event, *process.StepOutput) error
}

func (*recordingProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *recordingProcess) Step(events []process.Event, out *process.StepOutput) error {
	return p.step(events, out)
}
func (*recordingProcess) Close() {}

func newRouterDefinition(t *testing.T, proc *recordingProcess) (*Definition, *recordedEnvironment) {
	t.Helper()

	transcoder := syspayload.NewTranscoder()
	enginepayload.RegisterAllBasicFormats(transcoder)
	dc := dataconverter.NewDataConverter(transcoder)
	env := &recordedEnvironment{
		dc: dc,
		info: &workflow.Info{
			WorkflowExecution: workflow.Execution{ID: "self-workflow", RunID: "self-run"},
			TaskQueueName:     "default-queue",
		},
	}

	execCtx := ctxapi.NewRootContext()
	execCtx, frame := ctxapi.OpenFrameContext(execCtx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	values, err := ctxapi.GetOrCreateValues(execCtx)
	require.NoError(t, err)
	values.Set("trace", "literal-header")

	if proc == nil {
		proc = &recordingProcess{step: func(_ []process.Event, out *process.StepOutput) error {
			out.Idle()
			return nil
		}}
	}
	d := &Definition{
		env:       env,
		dc:        dc,
		proc:      proc,
		ctx:       context.Background(),
		execCtx:   execCtx,
		clientID:  "literal-client",
		workerID:  "literal-worker",
		replayLog: propagator.NewReplayLogger(zap.NewNop(), func() bool { return false }),
	}
	d.timers = NewTimerManager(env, d.replayLog, &d.signals)
	d.updates = NewUpdateManager(d.replayLog)
	return d, env
}

func jsonLiteral(value string) payload.Payload {
	return payload.NewPayload([]byte(`"`+value+`"`), payload.JSON)
}

func requireWireLiteral(t *testing.T, payloads *commonpb.Payloads, value string) {
	t.Helper()
	require.NotNil(t, payloads)
	require.Len(t, payloads.Payloads, 1)
	require.Equal(t, []byte(`"`+value+`"`), payloads.Payloads[0].Data)
}

func requireHeaderValue(t *testing.T, d *Definition, header *commonpb.Header, key, value string) {
	t.Helper()
	decoded, err := propagator.ExtractFromHeader(d.dc, header)
	require.NoError(t, err)
	require.Equal(t, value, decoded[key])
}

func requirePendingCompletion(t *testing.T, d *Definition, tag uint64) process.Event {
	t.Helper()
	require.Len(t, d.pendingCompletions, 1)
	event := d.pendingCompletions[0]
	require.Equal(t, process.EventYieldComplete, event.Type)
	require.Equal(t, tag, event.Tag)
	return event
}

func TestT01UnknownCommandFailsClosed(t *testing.T) {
	d, env := newRouterDefinition(t, nil)

	err := d.executeCommand(testCommand{id: 991}, 41)

	require.EqualError(t, err, "unknown command type: workflow.testCommand")
	require.Zero(t, env.actionCount())
}

func TestT02SleepRoutesCompletion(t *testing.T) {
	d, env := newRouterDefinition(t, nil)

	require.NoError(t, d.executeCommand(clockapi.SleepCmd{Duration: 137 * time.Millisecond}, 42))
	require.Len(t, env.timers, 1)
	require.Equal(t, 137*time.Millisecond, env.timers[0].duration)
	env.timers[0].callback(nil, nil)

	event := requirePendingCompletion(t, d, 42)
	require.NoError(t, event.Error)
	result, ok := event.Data.(payload.Payload)
	require.True(t, ok)
	require.Equal(t, true, result.Data())
}

func TestT03FunctionCallProjectsActivity(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	options := attrs.NewBag()
	options.Set("activity.task_queue", "literal-activity-queue")
	options.Set("activity.start_to_close_timeout", "17s")
	cmd := &function.CallCmd{Task: runtimeapi.Task{
		ID:       registry.NewID("literal", "activity"),
		Payloads: payload.Payloads{jsonLiteral("activity-input")},
		Options:  options,
	}}

	require.NoError(t, d.executeCommand(cmd, 43))
	require.Len(t, env.activities, 1)
	call := env.activities[0]
	require.Equal(t, "literal:activity", call.ActivityType.Name)
	require.Equal(t, "literal-activity-queue", call.TaskQueueName)
	require.Equal(t, 17*time.Second, call.StartToCloseTimeout)
	requireWireLiteral(t, call.Input, "activity-input")
	requireHeaderValue(t, d, call.Header, "trace", "literal-header")

	result, err := d.dc.ToPayloads(payload.Payloads{jsonLiteral("activity-result")})
	require.NoError(t, err)
	env.activityDone[0](result, nil)
	event := requirePendingCompletion(t, d, 43)
	callResult, ok := event.Data.(function.CallResult)
	require.True(t, ok)
	require.NoError(t, callResult.Error)
	require.Equal(t, payload.JSON, callResult.Value.(payload.Payload).Format())
	require.Equal(t, []byte(`"activity-result"`), callResult.Value.(payload.Payload).Data())
}

func TestT04ProcessSelfSendIsLocal(t *testing.T) {
	var resumed []process.Event
	proc := &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		resumed = append(resumed, events...)
		out.Idle()
		return nil
	}}
	d, env := newRouterDefinition(t, proc)
	cmd := &process.SendCmd{
		To:       pid.PID{Node: "ignored-node", Host: "literal-worker", UniqID: "self-workflow"},
		Topic:    "literal-self-topic",
		Payloads: payload.Payloads{jsonLiteral("self-input")},
	}

	require.NoError(t, d.executeCommand(cmd, 44))
	require.Zero(t, env.actionCount())
	require.Len(t, resumed, 1)
	require.Equal(t, uint64(44), resumed[0].Tag)
	result, ok := resumed[0].Data.(process.SendResult)
	require.True(t, ok)
	require.NoError(t, result.Error)
}

func TestT05ProcessTemporalSendProjectsSignal(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	cmd := &process.SendCmd{
		To:       pid.PID{Node: "literal-client", Host: "temporal", UniqID: "target-workflow"},
		Topic:    "literal-signal-topic",
		Payloads: payload.Payloads{jsonLiteral("signal-input")},
	}

	require.NoError(t, d.executeCommand(cmd, 45))
	require.Len(t, env.signals, 1)
	signal := env.signals[0]
	require.Empty(t, signal.namespace)
	require.Equal(t, "target-workflow", signal.workflowID)
	require.Empty(t, signal.runID)
	require.Equal(t, "literal-signal-topic", signal.topic)
	requireWireLiteral(t, signal.input, "signal-input")
	require.Nil(t, signal.arg)
	require.False(t, signal.childWorkflowOnly)
	requireHeaderValue(t, d, signal.header, "trace", "literal-header")
	requireHeaderValue(t, d, signal.header, "temporal.signal.from", "{literal-client@literal-worker|self-workflow}")

	signal.callback(nil, nil)
	event := requirePendingCompletion(t, d, 45)
	result, ok := event.Data.(process.SendResult)
	require.True(t, ok)
	require.NoError(t, result.Error)
}

func TestT06WorkflowExecProjectsChild(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	cmd := &workflowapi.ExecCmd{
		ID:   registry.NewID("literal", "child"),
		Args: payload.Payloads{jsonLiteral("child-input")},
		Options: &workflowapi.ExecOptions{
			WorkflowID:       "literal-child-id",
			TaskQueue:        "literal-child-queue",
			ExecutionTimeout: "2m",
			RunTimeout:       "1m",
			TaskTimeout:      "11s",
		},
	}

	require.NoError(t, d.executeCommand(cmd, 46))
	require.Len(t, env.children, 1)
	child := env.children[0]
	require.Equal(t, "literal:child", child.WorkflowType.Name)
	require.Equal(t, "literal-child-id", child.WorkflowID)
	require.Equal(t, "literal-child-queue", child.TaskQueueName)
	require.Equal(t, 2*time.Minute, child.WorkflowExecutionTimeout)
	require.Equal(t, time.Minute, child.WorkflowRunTimeout)
	require.Equal(t, 11*time.Second, child.WorkflowTaskTimeout)
	requireWireLiteral(t, child.Input, "child-input")
	requireHeaderValue(t, d, child.Header, "trace", "literal-header")

	result, err := d.dc.ToPayloads(payload.Payloads{jsonLiteral("child-result")})
	require.NoError(t, err)
	env.childDone[0](result, nil)
	event := requirePendingCompletion(t, d, 46)
	execResult, ok := event.Data.(workflowapi.ExecResult)
	require.True(t, ok)
	require.NoError(t, execResult.Error)
	require.Equal(t, payload.JSON, execResult.Value.Format())
	require.Equal(t, []byte(`"child-result"`), execResult.Value.Data())
}

func TestT07VersionRoutesBounds(t *testing.T) {
	var resumed []process.Event
	proc := &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		resumed = append(resumed, events...)
		out.Idle()
		return nil
	}}
	d, env := newRouterDefinition(t, proc)
	env.version = workflow.Version(23)

	require.NoError(t, d.executeCommand(&workflowapi.VersionCmd{
		ChangeID: "literal-change", MinSupported: 7, MaxSupported: 29,
	}, 47))

	require.Equal(t, []versionCall{{changeID: "literal-change", min: 7, max: 29}}, env.versions)
	require.Len(t, resumed, 1)
	require.Equal(t, uint64(47), resumed[0].Tag)
	require.Equal(t, workflowapi.VersionResult{Version: 23}, resumed[0].Data)
}

func TestT08UnsupportedMonitorIsNonRetryable(t *testing.T) {
	var resumed []process.Event
	proc := &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		resumed = append(resumed, events...)
		out.Idle()
		return nil
	}}
	d, env := newRouterDefinition(t, proc)

	require.NoError(t, d.executeCommand(&process.MonitorCmd{}, 48))
	require.Zero(t, env.actionCount())
	require.Len(t, resumed, 1)
	require.Equal(t, uint64(48), resumed[0].Tag)
	require.Nil(t, resumed[0].Data)
	var invalid apierror.Error
	require.ErrorAs(t, resumed[0].Error, &invalid)
	require.Equal(t, apierror.Invalid, invalid.Kind())
	require.Equal(t, apierror.False, invalid.Retryable())
}

func TestT09StepErrorCompletesOnce(t *testing.T) {
	sentinel := errors.New("literal step failure")
	proc := &recordingProcess{step: func([]process.Event, *process.StepOutput) error { return sentinel }}
	d, env := newRouterDefinition(t, proc)

	d.stepProcess([]process.Event{{Type: process.EventMessage}})

	require.True(t, d.completed)
	require.Len(t, env.completions, 1)
	require.Nil(t, env.completions[0].result)
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, env.completions[0].err, &applicationErr)
	require.Equal(t, "Internal", applicationErr.Type())
	require.Contains(t, applicationErr.Error(), "literal step failure")
}

func TestT10SynchronousCompletionStopsMultiYield(t *testing.T) {
	proc := &recordingProcess{step: func(_ []process.Event, out *process.StepOutput) error {
		out.Yield(testCommand{id: 992}, 50)
		out.Yield(clockapi.SleepCmd{Duration: time.Minute}, 51)
		out.WaitForYields()
		return nil
	}}
	d, env := newRouterDefinition(t, proc)

	d.OnWorkflowTaskStarted(0)

	require.True(t, d.completed)
	require.Len(t, env.completions, 1)
	var applicationErr *temporal.ApplicationError
	require.ErrorAs(t, env.completions[0].err, &applicationErr)
	require.Contains(t, applicationErr.Error(), "unknown command type: workflow.testCommand")
	require.Empty(t, env.timers, "the second yield must not dispatch after synchronous completion")
}
