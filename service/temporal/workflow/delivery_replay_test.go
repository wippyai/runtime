// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
	sdkw "go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/durationpb"
)

type deliveryFactory struct{ create process.FactoryFunc }

func (f deliveryFactory) Create(registry.ID) (process.Process, *process.Meta, error) {
	p, err := f.create()
	return p, nil, err
}

type deliveryHistory struct {
	dc     converter.DataConverter
	t      *testing.T
	queue  *taskqueuepb.TaskQueue
	events []*historypb.HistoryEvent
}

func newDeliveryHistory(t *testing.T, dc converter.DataConverter, name string, header *commonpb.Header) *deliveryHistory {
	h := &deliveryHistory{t: t, dc: dc, queue: &taskqueuepb.TaskQueue{Name: "test-queue"}}
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED, Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{WorkflowType: &commonpb.WorkflowType{Name: name}, TaskQueue: h.queue, Header: header}}})
	return h
}
func (h *deliveryHistory) add(event *historypb.HistoryEvent) int64 {
	event.EventId = int64(len(h.events) + 1)
	h.events = append(h.events, event)
	return event.EventId
}
func (h *deliveryHistory) task() (int64, int64) {
	scheduled := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED, Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: h.queue}}})
	started := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED, Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: scheduled}}})
	done := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED, Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: scheduled, StartedEventId: started}}})
	return started, done
}
func (h *deliveryHistory) signal(topic string) {
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED, Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{SignalName: topic}}})
}
func (h *deliveryHistory) replay(ctx context.Context, id registry.ID) {
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: h.dc})
	require.NoError(h.t, err)
	replayer.RegisterWorkflowWithOptions((&DefinitionFactory{ID: id, log: zap.NewNop()}).WithContext(ctx), sdkw.RegisterOptions{Name: id.String()})
	require.NoError(h.t, replayer.ReplayWorkflowHistory(nil, &historypb.History{Events: h.events}))
}

func TestAcceptedEventDeliveryReplaysDeterministically(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	id := registry.NewID("test", "delivery_replay")
	h := newDeliveryHistory(t, env.dc, id.String(), nil)
	for i := 0; i < 10002; i++ {
		h.signal("data")
	}
	_, task := h.task()
	count := 10002
	timerID := fmt.Sprint(len(h.events) + 1)
	timerEvent := h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_TIMER_STARTED, Attributes: &historypb.HistoryEvent_TimerStartedEventAttributes{TimerStartedEventAttributes: &historypb.TimerStartedEventAttributes{TimerId: timerID, StartToFireTimeout: durationpb.New(time.Duration(count) * time.Second), WorkflowTaskCompletedEventId: task}}})
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_TIMER_FIRED, Attributes: &historypb.HistoryEvent_TimerFiredEventAttributes{TimerFiredEventAttributes: &historypb.TimerFiredEventAttributes{TimerId: timerID, StartedEventId: timerEvent}}})
	_, task = h.task()
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED, Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{WorkflowTaskCompletedEventId: task}}})
	ctx := process.WithFactory(d.execCtx, deliveryFactory{create: func() (process.Process, error) {
		first := true
		return &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
			if !first {
				out.Done(nil)
				return nil
			}
			first = false
			messages := 0
			for _, event := range events {
				if event.Type == process.EventMessage {
					require.Equal(t, "data", event.Data.(*relay.Package).Messages[0].Topic)
					messages++
				}
			}
			require.Equal(t, count, messages)
			out.Yield(clockapi.SleepCmd{Duration: time.Duration(messages) * time.Second}, 1)
			out.WaitForYields()
			return nil
		}}, nil
	}})
	h.replay(ctx, id)
}
