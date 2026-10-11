// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	temporaloptions "github.com/wippyai/runtime/service/temporal/options"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.temporal.io/sdk/temporal"
)

type cancelingActivityEnvironment struct {
	*recordedEnvironment
	cancellations int
}

func (e *cancelingActivityEnvironment) RequestCancelActivity(_ bindings.ActivityID) {
	e.cancellations++
}

func TestAsyncActivityCancelPublishesOnlyActualCompletion(t *testing.T) {
	for _, outcome := range []string{"success", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			var messages, acknowledgements int
			proc := &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
				for _, event := range events {
					if event.Type == process.EventMessage {
						messages++
					}
					if event.Type == process.EventYieldComplete {
						acknowledgements++
					}
				}
				out.Idle()
				return nil
			}}
			d, recorded := newRouterDefinition(t, proc)
			env := &cancelingActivityEnvironment{recordedEnvironment: recorded}
			d.env = env
			start := function.AcquireAsyncStartCmd()
			defer start.Release()
			start.Topic = "async-activity"
			options := attrs.NewBag()
			options.Set(temporaloptions.OptionActivityWaitForCancel, false)
			start.Task = runtimeapi.Task{ID: registry.NewID("test", "activity"), Options: options}
			require.NoError(t, d.executeFunctionAsyncStart(start, 1))
			cancel := function.AcquireAsyncCancelCmd()
			defer cancel.Release()
			cancel.Topic = start.Topic
			require.NoError(t, d.executeFunctionAsyncCancel(cancel, 2))
			require.Equal(t, 1, env.cancellations)
			require.Zero(t, messages, "cancel published a terminal before activity completion")
			require.Empty(t, d.signals)
			require.Contains(t, d.asyncActivities, start.Topic, "cancellation discarded the pending outcome")
			require.Equal(t, 2, acknowledgements, "cancel must acknowledge without waiting for the activity")
			require.True(t, env.activities[0].WaitForCancellation, "SDK must wait for activity cancellation completion")
			if outcome == "canceled" {
				env.activityDone[0](nil, temporal.NewCanceledError())
			} else {
				wire, err := d.dc.ToPayloads(payload.Payloads{jsonLiteral("completed")})
				require.NoError(t, err)
				env.activityDone[0](wire, nil)
			}
			require.Len(t, d.signals, 1)
			require.Len(t, d.signals[0].Payloads, 2)
			require.True(t, payload.IsTerminal(d.signals[0].Payloads[1]))
			if outcome == "canceled" {
				require.Equal(t, payload.GoError, d.signals[0].Payloads[0].Format())
			} else {
				require.Equal(t, jsonLiteral("completed").Data(), d.signals[0].Payloads[0].Data())
			}
			require.Empty(t, d.asyncActivities)
			require.NoError(t, d.executeFunctionAsyncCancel(cancel, 3))
			require.Equal(t, 1, env.cancellations)
			require.Zero(t, messages, "completed activity acquired another terminal")
			require.Len(t, d.signals, 1)
		})
	}
}
