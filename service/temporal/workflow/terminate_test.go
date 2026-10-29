// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/durationpb"
)

type terminationEnvironment struct {
	*recordedEnvironment
	cancellations int
}

func (e *terminationEnvironment) RequestCancelExternalWorkflow(_, _, _ string, done bindings.ResultHandler) {
	e.cancellations++
	done(nil, nil)
}

func TestWorkflowTerminateSchedulesTerminationInsteadOfCancellation(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	terminating := &terminationEnvironment{recordedEnvironment: env}
	d.env = terminating
	require.NoError(t, d.executeProcessTerminate(&process.TerminateCmd{Target: pid.PID{UniqID: "target"}}, 42))
	require.Equal(t, 0, terminating.cancellations)
	require.Empty(t, env.versions)
	require.Len(t, env.activities, 1)
	require.Equal(t, "__wippy_terminate_workflow", env.activities[0].ActivityType.Name)
	var target string
	require.NoError(t, d.dc.FromPayloads(env.activities[0].Input, &target))
	require.Equal(t, "target", target)
	require.Empty(t, d.pendingCompletions)
	env.activityDone[0](nil, nil)
	requirePendingCompletion(t, d, 42)
	require.NoError(t, d.pendingCompletions[0].Error)
}

func TestWorkflowTerminateSurfacesActivityFailure(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	d.env = &terminationEnvironment{recordedEnvironment: env}
	require.NoError(t, d.executeProcessTerminate(&process.TerminateCmd{Target: pid.PID{UniqID: "target"}}, 42))
	require.Empty(t, env.versions)
	require.Len(t, env.activities, 1)
	env.activityDone[0](nil, temporal.NewApplicationError("denied", "permission_denied"))
	require.Error(t, requirePendingCompletion(t, d, 42).Error)
}

func TestWorkflowCancelRetainsCancellation(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	canceling := &terminationEnvironment{recordedEnvironment: env}
	d.env = canceling
	require.NoError(t, d.executeProcessCancel(&process.CancelCmd{Target: pid.PID{UniqID: "target"}}, 1))
	require.Equal(t, 1, canceling.cancellations)
	require.Empty(t, env.activities)
}

func TestTerminatedChildExitUsesExistingErrorKind(t *testing.T) {
	d, recorded := newRouterDefinition(t, nil)
	d.env = &childStartedEnvironment{recorded}
	require.NoError(t, d.executeProcessSpawn(&process.SpawnCmd{Start: &process.Start{Source: registry.NewID("test", "child")}}, 1))
	recorded.childDone[0](nil, &temporal.TerminatedError{})
	require.Len(t, d.childExits, 1)
	var rich apierror.Rich
	require.True(t, errors.As(d.childExits[0].Error, &rich))
	require.Equal(t, apierror.Canceled, rich.Kind())
	require.Equal(t, "workflow terminated", rich.Error())
}

func TestWorkflowTerminationReplaysDeterministically(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	id := registry.NewID("test", "terminate_replay")
	h := newDeliveryHistory(t, env.dc, id.String(), nil)
	_, task := h.task()
	input, err := h.dc.ToPayloads("target")
	require.NoError(t, err)
	scheduled := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED, Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{ActivityId: fmt.Sprint(len(h.events) + 1), ActivityType: &commonpb.ActivityType{Name: "__wippy_terminate_workflow"}, TaskQueue: h.queue, Input: input, StartToCloseTimeout: durationpb.New(defaultActivityTimeout), RetryPolicy: &commonpb.RetryPolicy{MaximumAttempts: 1}, WorkflowTaskCompletedEventId: task}}})
	activityStart := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED, Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: scheduled}}})
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED, Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{ScheduledEventId: scheduled, StartedEventId: activityStart}}})
	_, task = h.task()
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED, Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{WorkflowTaskCompletedEventId: task}}})
	ctx := process.WithFactory(d.execCtx, deliveryFactory{create: func() (process.Process, error) {
		first := true
		return &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
			if first {
				first = false
				out.Yield(&process.TerminateCmd{Target: pid.PID{UniqID: "target"}}, 1)
				out.WaitForYields()
				return nil
			}
			require.Len(t, events, 1)
			require.NoError(t, events[0].Error)
			out.Done(nil)
			return nil
		}}, nil
	}})
	h.replay(ctx, id)
}
