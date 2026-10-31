// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
)

// Commands emitted by the shipped implementation must still replay after a
// worker upgrade, even though new termination calls no longer cancel.
func TestLegacyTerminateHistoryReplaysAfterUpgrade(t *testing.T) {
	d, env := newRouterDefinition(t, nil)
	id := registry.NewID("test", "legacy_terminate_replay")
	h := newDeliveryHistory(t, env.dc, id.String(), nil)
	_, task := h.task()
	execution := &commonpb.WorkflowExecution{WorkflowId: "target"}
	initiated := h.add(&historypb.HistoryEvent{
		EventType: enumspb.EVENT_TYPE_REQUEST_CANCEL_EXTERNAL_WORKFLOW_EXECUTION_INITIATED,
		Attributes: &historypb.HistoryEvent_RequestCancelExternalWorkflowExecutionInitiatedEventAttributes{
			RequestCancelExternalWorkflowExecutionInitiatedEventAttributes: &historypb.RequestCancelExternalWorkflowExecutionInitiatedEventAttributes{
				WorkflowTaskCompletedEventId: task, WorkflowExecution: execution,
				Control: fmt.Sprint(len(h.events) + 1), //nolint:staticcheck // Legacy server histories contain the SDK cancellation control field.
			},
		},
	})
	h.add(&historypb.HistoryEvent{
		EventType: enumspb.EVENT_TYPE_EXTERNAL_WORKFLOW_EXECUTION_CANCEL_REQUESTED,
		Attributes: &historypb.HistoryEvent_ExternalWorkflowExecutionCancelRequestedEventAttributes{
			ExternalWorkflowExecutionCancelRequestedEventAttributes: &historypb.ExternalWorkflowExecutionCancelRequestedEventAttributes{
				InitiatedEventId: initiated, WorkflowExecution: execution,
			},
		},
	})
	_, task = h.task()
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
		Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{
			WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{WorkflowTaskCompletedEventId: task},
		}})
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
