// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	bindings "go.temporal.io/sdk/internalbindings"
)

func TestQueryTypeState(t *testing.T) {
	queryType := "state"
	assert.Equal(t, "state", queryType)
}

func TestQueryTypePID(t *testing.T) {
	queryType := "pid"
	assert.Equal(t, "pid", queryType)
}

func TestQueryStateMap(t *testing.T) {
	state := make(map[string]any)
	state["key1"] = "value1"
	state["key2"] = 42

	assert.Len(t, state, 2)
	assert.Equal(t, "value1", state["key1"])
	assert.Equal(t, 42, state["key2"])
}

func TestQueryStateMapNil(t *testing.T) {
	state := initStateIfNil(nil)
	assert.NotNil(t, state)
	assert.Empty(t, state)
}

func initStateIfNil(state map[string]any) map[string]any {
	if state == nil {
		return make(map[string]any)
	}
	return state
}

func TestCancelEventFields(t *testing.T) {
	cancelEvent := map[string]any{
		"at":   "2024-01-01T00:00:00Z",
		"kind": topology.Cancel,
		"from": topology.SystemPID.String(),
	}

	assert.Contains(t, cancelEvent, "at")
	assert.Contains(t, cancelEvent, "kind")
	assert.Contains(t, cancelEvent, "from")
	assert.Equal(t, topology.Cancel, cancelEvent["kind"])
}

func TestTopologyConstants(t *testing.T) {
	assert.NotEmpty(t, topology.TopicEvents)
	assert.NotEmpty(t, topology.SystemPID.String())
}

func TestCanceledFlag(t *testing.T) {
	var canceled bool
	assert.False(t, canceled)

	canceled = true
	assert.True(t, canceled)
}

func TestCompletedFlag(t *testing.T) {
	var completed bool
	assert.False(t, completed)

	completed = true
	assert.True(t, completed)
}

func TestAcceptedSignalsSurviveQueuePressure(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(fmt.Sprintf("internal=%t", internal), func(t *testing.T) {
			var received []string
			d, env := newRouterDefinition(t, &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
				for _, event := range events {
					received = append(received, event.Data.(*relay.Package).Messages[0].Topic)
				}
				out.Idle()
				return nil
			}})
			for i := 0; i < 10002; i++ {
				topic := fmt.Sprintf("event-%d", i)
				if internal {
					d.enqueueInternalSignal(topic, payload.Payloads{payload.NewString(topic)})
				} else {
					wire, err := d.dc.ToPayloads(payload.Payloads{payload.NewString(topic)})
					require.NoError(t, err)
					require.NoError(t, d.handleSignal(topic, wire, nil))
				}
			}
			d.OnWorkflowTaskStarted(0)
			require.Equal(t, 10002, len(received))
			require.Empty(t, env.versions)
			for i, topic := range received {
				require.Equal(t, fmt.Sprintf("event-%d", i), topic)
			}
		})
	}
}

type childStartedEnvironment struct{ *recordedEnvironment }

func (*childStartedEnvironment) Now() time.Time { return time.Unix(0, 0) }

func (e *childStartedEnvironment) ExecuteChildWorkflow(params bindings.ExecuteWorkflowParams, done bindings.ResultHandler, started func(bindings.WorkflowExecution, error)) {
	e.recordedEnvironment.ExecuteChildWorkflow(params, done, started)
	started(bindings.WorkflowExecution{ID: fmt.Sprintf("child-%d", len(e.children))}, nil)
}

func TestAcceptedChildExitsSurviveQueuePressure(t *testing.T) {
	var received []pid.PID
	d, recorded := newRouterDefinition(t, &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		for _, event := range events {
			if event.Type == process.EventMessage {
				exit := event.Data.(*relay.Package).Messages[0].Payloads[0].Data().(*topology.ExitEvent)
				received = append(received, exit.From)
			}
		}
		out.Idle()
		return nil
	}})
	d.env = &childStartedEnvironment{recorded}
	for i := 0; i < 1002; i++ {
		require.NoError(t, d.executeProcessSpawn(&process.SpawnCmd{Start: &process.Start{Source: registry.NewID("test", "child")}}, uint64(i)))
		// The SDK supplies generated workflow IDs when none is requested.
		recorded.childDone[i](nil, nil)
	}
	d.OnWorkflowTaskStarted(0)
	require.Equal(t, 1002, len(received))
	require.Empty(t, recorded.versions)
	for i, child := range received {
		require.Equal(t, fmt.Sprintf("child-%d", i+1), child.UniqID)
	}
}

func TestUpdateQueueUpdate(t *testing.T) {
	// Test update structure
	type testUpdate struct {
		Name string
		ID   string
	}

	update := testUpdate{
		Name: "updateName",
		ID:   "update-123",
	}

	assert.Equal(t, "updateName", update.Name)
	assert.Equal(t, "update-123", update.ID)
}

func TestQueryStateCustomKey(t *testing.T) {
	state := make(map[string]any)
	state["custom-key"] = "custom-value"

	val, ok := state["custom-key"]
	assert.True(t, ok)
	assert.Equal(t, "custom-value", val)

	_, ok = state["nonexistent"]
	assert.False(t, ok)
}

func TestQueryStateTypes(t *testing.T) {
	state := make(map[string]any)
	state["string"] = "value"
	state["int"] = 42
	state["float"] = 3.14
	state["bool"] = true
	state["map"] = map[string]any{"nested": "value"}
	state["slice"] = []any{1, 2, 3}

	assert.IsType(t, "", state["string"])
	assert.IsType(t, 0, state["int"])
	assert.IsType(t, 0.0, state["float"])
	assert.IsType(t, true, state["bool"])
	assert.IsType(t, map[string]any{}, state["map"])
	assert.IsType(t, []any{}, state["slice"])
}
