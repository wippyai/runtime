// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"go.temporal.io/sdk/workflow"
)

func TestLegacySignalOverflowPreservesUncappedEvents(t *testing.T) {
	var received []string
	d, env := newRouterDefinition(t, &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		for _, event := range events {
			received = append(received, event.Data.(*relay.Package).Messages[0].Topic)
		}
		out.Idle()
		return nil
	}})
	env.version = workflow.DefaultVersion
	for i := 0; i < legacySignalLimit+1; i++ {
		d.queueSignal(incomingSignal{Name: "ordinary"})
	}
	// Cancel and timer notifications did not use the former capped admission
	// path. They remain visible even if an earlier signal was discarded.
	d.signals = append(d.signals, incomingSignal{Name: "cancel"})
	d.queueSignal(incomingSignal{Name: "discarded"})
	d.signals = append(d.signals, incomingSignal{Name: "timer"})
	d.OnWorkflowTaskStarted(0)
	require.Len(t, received, legacySignalLimit+2)
	require.Equal(t, []string{"cancel", "timer"}, received[legacySignalLimit:])
	require.Empty(t, d.legacySignalDrops)
	// Each subsequent task starts with an empty admission budget.
	d.queueSignal(incomingSignal{Name: "next-task"})
	d.OnWorkflowTaskStarted(0)
	require.Equal(t, "next-task", received[len(received)-1])
}

func TestConsumedTemporalEventsReleaseReferencesWithoutVersionMarker(t *testing.T) {
	d, env := newRouterDefinition(t, &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
		require.Len(t, events, 3)
		out.Idle()
		return nil
	}})
	d.env = &childStartedEnvironment{env}
	d.enqueueYieldCompletion(1, []byte("activity result"), nil)
	d.queueSignal(incomingSignal{Name: "data", Payloads: payload.Payloads{payload.NewString("message")}})
	d.childExits = []childExitEvent{{ChildPID: pid.PID{UniqID: "child"}, Result: payload.NewString("child result")}}
	completions, signals, exits := d.pendingCompletions, d.signals, d.childExits
	d.OnWorkflowTaskStarted(time.Second)
	require.Zero(t, completions[0])
	require.Zero(t, signals[0])
	require.Zero(t, exits[0])
	require.Empty(t, env.versions, "ordinary delivery adds no recorded commands")
}
