// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	enumspb "go.temporal.io/api/enums/v1"
)

type terminationWorkflowPolicy struct{}

func (terminationWorkflowPolicy) ID() registry.ID {
	return registry.NewID("test.workflow", "termination-permissions")
}

func (terminationWorkflowPolicy) Evaluate(_ security.Actor, action, _ string, _ attrs.Bag) security.Result {
	switch action {
	case "process.spawn", "process.spawn.monitored", "process.host", "process.send", "process.terminate":
		return security.Allow
	default:
		return security.Deny
	}
}

func TestWorkflowTermination_Integration(t *testing.T) {
	// The fixture constructs the engine without the boot error metadata setup.
	glua.SetErrorMetadataExtractor(func(err error) *glua.ErrorMetadata {
		chain := apierror.BuildChain(err)
		if chain == nil || chain.Root() == nil {
			return nil
		}
		root := chain.Root()
		return &glua.ErrorMetadata{Kind: glua.Kind(root.Kind), Retryable: root.Retryable, Details: root.Details}
	})
	t.Cleanup(func() { glua.SetErrorMetadataExtractor(nil) })
	id := registry.NewID("test.workflow", "termination")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		workflowID: id, taskQueue: "test-termination-queue",
		modules: []*luaapi.ModuleDef{processmod.Module}, allowedClasses: processWorkflowClasses,
		policies:   []security.Policy{terminationWorkflowPolicy{}},
		engineOpts: []engine.FactoryOption{engine.WithModule(processmod.Module)}, imports: []code.Import{{ID: registry.NewID("", "process"), Alias: "process"}},
		source: `local process=require("process")
return function(input)
 local events=process.events()
 if input and input.child then
  assert(process.send(input.parent,"ready",true))
  events:receive()
  return {cleanup=true}
 end
 local ready=process.listen("ready")
 local child,err=process.spawn_monitored("test.workflow:termination","",{child=true,parent=process.pid()})
 assert(child and not err,tostring(err))
 ready:receive()
 local ok,terminate_err=process.terminate(child)
 assert(ok and not terminate_err,tostring(terminate_err))
 local exit=events:receive()
 assert(exit.kind==process.event.EXIT)
 assert(exit.from==child)
 assert(exit.result.error,"termination must not return the cancellation cleanup result")
 return {child=child,error=tostring(exit.result.error),error_kind=exit.result.error:kind()}
end`,
	})
	defer f.cleanup()
	run := f.startWorkflow(id.String(), nil)
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	var result map[string]any
	require.NoError(t, run.Get(ctx, &result))
	require.Contains(t, result["error"], "workflow terminated")
	require.Equal(t, "Canceled", result["error_kind"])
	target, err := pid.ParsePID(result["child"].(string))
	require.NoError(t, err)
	description, err := f.temporalClient.DescribeWorkflowExecution(f.ctx, target.UniqID, "")
	require.NoError(t, err)
	require.Equal(t, enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED, description.WorkflowExecutionInfo.Status)
	events := f.temporalClient.GetWorkflowHistory(f.ctx, target.UniqID, "", false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for events.HasNext() {
		event, err := events.Next()
		require.NoError(t, err)
		require.NotEqual(t, enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CANCEL_REQUESTED, event.EventType)
		require.NotEqual(t, enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED, event.EventType)
	}
	replayLiveWorkflowChain(t, f, id, run.GetID(), run.GetRunID(), []byte("0123456789abcdef0123456789abcdef"))
}
