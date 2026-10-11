// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	securityapi "github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	funcsmod "github.com/wippyai/runtime/runtime/lua/modules/funcs"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	wippyworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
)

type replayProcessFactory struct {
	create process.FactoryFunc
}

func (f replayProcessFactory) Create(registry.ID) (process.Process, *process.Meta, error) {
	proc, err := f.create()
	return proc, nil, err
}

// Replay the pre-fix command sequence through the SDK and the real Lua process.
// A successor activity is scheduled before the canceled activity completes.
// Both reading result() immediately and receiving on the future were supported.
func TestAsyncCancellationReplaysExistingWorkflowHistory(t *testing.T) {
	for _, wait := range []bool{false, true} {
		for _, receive := range []bool{false, true} {
			t.Run(fmt.Sprintf("wait_for_cancellation=%t/receive=%t", wait, receive), func(t *testing.T) {
				wfID := registry.NewID("test.workflow", "async_cancel_replay")
				ctx := ctxapi.NewRootContext()
				ctx = securityapi.SetStrictMode(ctx, false)
				transcoder := newTestTranscoder()
				payload.WithTranscoder(ctx, transcoder)
				dc := dataconverter.NewDataConverter(transcoder)
				manager, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{
					Modules: []*luaapi.ModuleDef{funcsmod.Module, timemod.Module},
				})
				require.NoError(t, err)
				source := fmt.Sprintf(`
local funcs = require("funcs")
local time = require("time")
return function()
    local executor = funcs.new():with_options({["activity.wait_for_cancellation"] = %t})
    local future, start_err = executor:async("test:slow")
    assert(future and not start_err, tostring(start_err))
    time.sleep(time.SECOND)
    local ok, cancel_err = future:cancel()
    assert(ok and not cancel_err)
    if %t then future:response():receive() end
    local _, err = future:result()
    local successor = err and "test:after_cancel" or "test:unexpected"
    return funcs.call(successor)
end
`, wait, receive)
				require.NoError(t, manager.AddNode(ctx, code.Node{
					ID: wfID, Kind: luaapi.Workflow, Method: "main", Source: source,
				}, []code.Import{
					{ID: registry.NewID("", "funcs"), Alias: "funcs"},
					{ID: registry.NewID("", "time"), Alias: "time"},
				}))
				factory, err := engine.NewProcessFactory(manager).CreateFactory(wfID,
					engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow))
				require.NoError(t, err)
				ctx = process.WithFactory(ctx, replayProcessFactory{create: factory})
				replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: dc})
				require.NoError(t, err)
				replayer.RegisterWorkflowWithOptions((&wippyworkflow.DefinitionFactory{ID: wfID}).WithContext(ctx),
					workflow.RegisterOptions{Name: wfID.String()})
				require.NoError(t, replayer.ReplayWorkflowHistory(nil, asyncCancellationHistory(t, dc, wfID.String())))
			})
		}
	}
}

// This fixture has no version markers. Its commands are also replay-checked on
// the pre-#961 runtime: schedule slow activity and timer, cancel slow, then
// schedule the successor without waiting for an ActivityTaskCanceled event.
func asyncCancellationHistory(t *testing.T, dc converter.DataConverter, workflowName string) *historypb.History {
	t.Helper()
	queue := &taskqueuepb.TaskQueue{Name: "async-cancel-replay"}
	result, err := dc.ToPayloads(payload.Payloads{payload.NewString("done")})
	require.NoError(t, err)
	return &historypb.History{Events: []*historypb.HistoryEvent{
		{EventId: 1, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{
				WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
					WorkflowType: &commonpb.WorkflowType{Name: workflowName}, TaskQueue: queue,
				}}},
		{EventId: 2, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{
				WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: queue}}},
		{EventId: 3, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{
				WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 2}}},
		{EventId: 4, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{
				WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}}},
		{EventId: 5, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{
				ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
					ActivityId: "5", ActivityType: &commonpb.ActivityType{Name: "test:slow"}, TaskQueue: queue,
					StartToCloseTimeout: durationpb.New(10 * time.Minute), WorkflowTaskCompletedEventId: 4,
				}}},
		{EventId: 6, EventType: enumspb.EVENT_TYPE_TIMER_STARTED,
			Attributes: &historypb.HistoryEvent_TimerStartedEventAttributes{
				TimerStartedEventAttributes: &historypb.TimerStartedEventAttributes{
					TimerId: "6", StartToFireTimeout: durationpb.New(time.Second), WorkflowTaskCompletedEventId: 4,
				}}},
		{EventId: 7, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{
				ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: 5}}},
		{EventId: 8, EventType: enumspb.EVENT_TYPE_TIMER_FIRED,
			Attributes: &historypb.HistoryEvent_TimerFiredEventAttributes{
				TimerFiredEventAttributes: &historypb.TimerFiredEventAttributes{TimerId: "6", StartedEventId: 6}}},
		{EventId: 9, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{
				WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: queue}}},
		{EventId: 10, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{
				WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 9}}},
		{EventId: 11, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{
				WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 9, StartedEventId: 10}}},
		{EventId: 12, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_CANCEL_REQUESTED,
			Attributes: &historypb.HistoryEvent_ActivityTaskCancelRequestedEventAttributes{
				ActivityTaskCancelRequestedEventAttributes: &historypb.ActivityTaskCancelRequestedEventAttributes{
					ScheduledEventId: 5, WorkflowTaskCompletedEventId: 11,
				}}},
		{EventId: 13, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{
				ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
					ActivityId: "13", ActivityType: &commonpb.ActivityType{Name: "test:after_cancel"}, TaskQueue: queue,
					StartToCloseTimeout: durationpb.New(10 * time.Minute), WorkflowTaskCompletedEventId: 11,
				}}},
		{EventId: 14, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{
				ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: 13}}},
		{EventId: 15, EventType: enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{
				ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{
					ScheduledEventId: 13, StartedEventId: 14, Result: result,
				}}},
		{EventId: 16, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{
				WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: queue}}},
		{EventId: 17, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{
				WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 16}}},
		{EventId: 18, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{
				WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 16, StartedEventId: 17}}},
		{EventId: 19, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{
				WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{
					WorkflowTaskCompletedEventId: 18, Result: result,
				}}},
	}}
}
