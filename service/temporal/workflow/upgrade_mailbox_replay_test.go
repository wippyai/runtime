// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/service/temporal/propagator"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
)

func TestContinueAsNewReplaysDeterministically(t *testing.T) {
	d, env := luaMailboxDefinition(t, `local process=require("process"); return function() local data=process.listen("data"); local gate=process.listen("upgrade"); gate:receive(); process.upgrade() end`, nil)
	h := newDeliveryHistory(t, env.dc, d.id.String(), nil)
	h.signal("data")
	h.signal("upgrade")
	_, task := h.task()
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CONTINUED_AS_NEW, Attributes: &historypb.HistoryEvent_WorkflowExecutionContinuedAsNewEventAttributes{WorkflowExecutionContinuedAsNewEventAttributes: &historypb.WorkflowExecutionContinuedAsNewEventAttributes{WorkflowType: &commonpb.WorkflowType{Name: d.id.String()}, TaskQueue: h.queue, WorkflowTaskCompletedEventId: task, NewExecutionRunId: "next-run"}}})
	h.replay(d.ctx, d.id)
}

func TestRestoredMailboxReplaysBeforeNewSignals(t *testing.T) {
	_, env := newRouterDefinition(t, nil)
	messages := []process.MailboxEnvelope{{Topic: "data", Payloads: payload.Payloads{payload.NewString("old")}}}
	header, err := propagator.AddMailboxToHeader(env.dc, nil, messages)
	require.NoError(t, err)
	d, _ := luaMailboxDefinition(t, `local process=require("process");return function() local data=process.listen("data");assert(data:receive()=="old");assert(data:receive()=="new");end`, nil)
	h := newDeliveryHistory(t, env.dc, d.id.String(), header)
	wire, err := env.dc.ToPayloads(payload.Payloads{payload.NewString("new")})
	require.NoError(t, err)
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED, Attributes: &historypb.HistoryEvent_WorkflowExecutionSignaledEventAttributes{WorkflowExecutionSignaledEventAttributes: &historypb.WorkflowExecutionSignaledEventAttributes{SignalName: "data", Input: wire}}})
	_, task := h.task()
	h.add(&historypb.HistoryEvent{EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED, Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{WorkflowTaskCompletedEventId: task}}})
	h.replay(d.ctx, d.id)
}
