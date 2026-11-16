// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	xmlmod "github.com/wippyai/runtime/runtime/lua/modules/xml"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	wippyworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestXMLRunsInWorkflow replays a workflow that uses the xml module under the class
// filter workflows apply: only deterministic and workflow modules are admitted.
func TestXMLRunsInWorkflow(t *testing.T) {
	id := registry.NewID("test.workflow", "xml_replay")
	ctx := ctxapi.NewRootContext()
	transcoder := newTestTranscoder()
	payload.WithTranscoder(ctx, transcoder)
	dc := dataconverter.NewDataConverter(transcoder)
	manager, err := code.NewCodeManager(zap.NewNop(), nil, code.Config{Modules: []*luaapi.ModuleDef{xmlmod.Module}})
	require.NoError(t, err)
	require.NoError(t, manager.AddNode(ctx, code.Node{
		ID: id, Kind: luaapi.Workflow, Method: "main", Source: `
local xml = require("xml")
return function(doc)
    local tree, err = xml.decode(doc, {whitespace = "strip"})
    assert(tree and not err, tostring(err))
    local reader = assert(xml.reader(doc, {whitespace = "strip"}))
    local writer = assert(xml.writer())
    assert(writer:start_element("names"))
    while true do
        local kind, rerr = reader:next()
        assert(not rerr, tostring(rerr))
        if not kind then break end
        if kind == "start" and reader:name() == "item" then
            assert(writer:element("name", nil, reader:attr("name")))
        end
    end
    assert(writer:end_element())
    local names = assert(writer:finish())
    local records = assert(xml.reader(doc):select("order/item"))
    local batch = assert(records:read())
    assert(#batch == 2 and xml.attr(batch[2], "name") == "b")
    local out = assert(xml.encode(tree))
    local result = out .. "|" .. names
    assert(result == '<order id="7"><item name="a"/><item name="b"/></order>|<names><name>a</name><name>b</name></names>', result)
    return result
end`,
	}, []code.Import{{ID: registry.NewID("", "xml"), Alias: "xml"}}))
	factory, err := engine.NewProcessFactory(manager).CreateFactory(id, engine.WithAllowedClasses(luaapi.ClassDeterministic, luaapi.ClassWorkflow))
	require.NoError(t, err, "workflows admit the xml module")
	ctx = process.WithFactory(ctx, replayProcessFactory{create: factory})

	doc := `<order id="7"> <item name="a"/> <item name="b"/> </order>`
	input, err := dc.ToPayloads(payload.Payloads{payload.NewString(doc)})
	require.NoError(t, err)
	result, err := dc.ToPayloads(payload.Payloads{payload.NewString(
		`<order id="7"><item name="a"/><item name="b"/></order>|<names><name>a</name><name>b</name></names>`)})
	require.NoError(t, err)
	started := time.Unix(1700000000, 0)
	queue := &taskqueuepb.TaskQueue{Name: "xml-replay"}
	history := &historypb.History{Events: []*historypb.HistoryEvent{
		{EventId: 1, EventTime: timestamppb.New(started), EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{
				WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
					WorkflowType: &commonpb.WorkflowType{Name: id.String()}, TaskQueue: queue, Input: input,
				}}},
		{EventId: 2, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{
				WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{TaskQueue: queue}}},
		{EventId: 3, EventTime: timestamppb.New(started), EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{
				WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: 2}}},
		{EventId: 4, EventType: enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{
				WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: 2, StartedEventId: 3}}},
		{EventId: 5, EventType: enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED,
			Attributes: &historypb.HistoryEvent_WorkflowExecutionCompletedEventAttributes{
				WorkflowExecutionCompletedEventAttributes: &historypb.WorkflowExecutionCompletedEventAttributes{
					WorkflowTaskCompletedEventId: 4, Result: result,
				}}},
	}}
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: dc})
	require.NoError(t, err)
	replayer.RegisterWorkflowWithOptions((&wippyworkflow.DefinitionFactory{ID: id}).WithContext(ctx), workflow.RegisterOptions{Name: id.String()})
	require.NoError(t, replayer.ReplayWorkflowHistory(nil, history))
}
