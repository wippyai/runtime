// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/runtime/lua/code"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	workflowmod "github.com/wippyai/runtime/runtime/lua/modules/workflow"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/internal/securitykeys"
	"github.com/wippyai/runtime/service/temporal/propagator"
	wippyworkflow "github.com/wippyai/runtime/service/temporal/workflow"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	sdkworker "go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"
)

func TestContinuedMailboxAcrossRetry_Integration(t *testing.T) {
	for _, secured := range []bool{false, true} {
		for _, retry := range []bool{false, true} {
			t.Run(fmt.Sprintf("secured=%t/retry=%t", secured, retry), func(t *testing.T) {
				id := registry.NewID("test.workflow", "continued_mailbox")
				var policies []security.Policy
				if secured {
					policies = []security.Policy{terminationWorkflowPolicy{}}
				}
				f := newWorkflowTestFixture(t, workflowTestOpts{
					workflowID: id, taskQueue: "test-continued-mailbox", policies: policies,
					modules: []*luaapi.ModuleDef{processmod.Module, workflowmod.Module},
					imports: []code.Import{{ID: registry.NewID("", "process"), Alias: "process"}, {ID: registry.NewID("", "workflow"), Alias: "workflow"}},
					source: `local process=require("process")
local workflow=require("workflow")
return function(input)
 local inbox=process.listen("data",{message=true})
 if not input.continued then
  local gate=process.listen("upgrade")
  gate:receive()
  process.upgrade(nil,{continued=true,retry=input.retry})
 end
 local info=workflow.info()
 if input.retry and info.attempt==1 then
  return nil,errors.new({message="retry the continued execution",kind=errors.UNAVAILABLE,retryable=true})
 end
 local first=inbox:receive()
 local second=inbox:receive()
 return {first=first:data(),second=second:data(),attempt=info.attempt}
end`,
				})
				defer f.cleanup()
				ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
				defer cancel()
				run, err := f.temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
					ID: fmt.Sprintf("mailbox-%t-%t", secured, retry), TaskQueue: f.taskQueue,
					RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 2, InitialInterval: 10 * time.Millisecond},
				}, id.String(), map[string]any{"retry": retry})
				require.NoError(t, err)
				dc := dataconverter.NewDataConverter(newTestTranscoder())
				for _, signal := range []struct{ topic, value string }{{"data", "first"}, {"data", "second"}, {"upgrade", "go"}} {
					var header *commonpb.Header
					if secured {
						header, err = propagator.AddRelaySignalToHeader(dc, nil, propagator.RelaySignalTicket{
							Audience: run.GetID(), Signal: signal.topic, Operation: propagator.RelaySignalOperation,
						}, []byte("0123456789abcdef0123456789abcdef"))
						require.NoError(t, err)
					}
					input, err := dc.ToPayloads(payload.Payloads{payload.NewString(signal.value)})
					require.NoError(t, err)
					_, err = f.temporalClient.WorkflowService().SignalWorkflowExecution(ctx, &workflowservice.SignalWorkflowExecutionRequest{
						Namespace: "default", WorkflowExecution: &commonpb.WorkflowExecution{WorkflowId: run.GetID()},
						SignalName: signal.topic, Input: input, Header: header,
					})
					require.NoError(t, err)
				}
				var result map[string]any
				require.NoError(t, run.Get(ctx, &result))
				require.Equal(t, "first", result["first"])
				require.Equal(t, "second", result["second"])
				attempt := 1
				if retry {
					attempt = 2
				}
				requireNumericEqual(t, float64(attempt), result["attempt"])
				var keys [][]byte
				if secured {
					keys = [][]byte{[]byte("0123456789abcdef0123456789abcdef")}
				}
				replayLiveWorkflowChain(t, f, id, run.GetID(), run.GetRunID(), keys...)
			})
		}
	}
}

// Replay every recorded execution, including the first continued run and a
// failed retry attempt, rather than checking only the final workflow result.
func replayLiveWorkflowChain(t *testing.T, f *workflowTestFixture, id registry.ID, workflowID, runID string, keys ...[]byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	for executions := 0; executions < 10; executions++ {
		var history historypb.History
		iterator := f.temporalClient.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
		for iterator.HasNext() {
			event, err := iterator.Next()
			require.NoError(t, err)
			history.Events = append(history.Events, event)
		}
		require.NotEmpty(t, history.Events)
		replayer, err := sdkworker.NewWorkflowReplayerWithOptions(sdkworker.WorkflowReplayerOptions{DataConverter: dataconverter.NewDataConverter(newTestTranscoder())})
		require.NoError(t, err)
		replayer.RegisterWorkflowWithOptions((&wippyworkflow.DefinitionFactory{ID: id}).WithContext(securitykeys.WithKeys(f.ctx, keys...)), sdkworkflow.RegisterOptions{Name: id.String()})
		require.NoError(t, replayer.ReplayWorkflowHistoryWithOptions(nil, &history, sdkworker.ReplayWorkflowHistoryOptions{
			OriginalExecution: sdkworkflow.Execution{ID: workflowID, RunID: runID},
		}))
		last := history.Events[len(history.Events)-1]
		switch last.EventType {
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_CONTINUED_AS_NEW:
			runID = last.GetWorkflowExecutionContinuedAsNewEventAttributes().GetNewExecutionRunId()
		case enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_FAILED:
			runID = last.GetWorkflowExecutionFailedEventAttributes().GetNewExecutionRunId()
		default:
			require.Equal(t, enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_COMPLETED, last.EventType)
			return
		}
		require.NotEmpty(t, runID)
	}
	t.Fatal("workflow execution chain exceeded the fixture's expected length")
}
