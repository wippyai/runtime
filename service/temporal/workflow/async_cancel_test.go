// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
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

func TestAsyncActivityCancellationContractUnchanged(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprintf("wait_for_cancellation=%t", wait), func(t *testing.T) {
			var messages, acknowledgements int
			proc := &recordingProcess{step: func(events []process.Event, out *process.StepOutput) error {
				for _, event := range events {
					if event.Type == process.EventMessage {
						pkg := event.Data.(*relay.Package)
						require.Len(t, pkg.Messages, 1)
						require.Equal(t, "async-activity", pkg.Messages[0].Topic)
						require.Len(t, pkg.Messages[0].Payloads, 1)
						require.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[0]))
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
			options.Set(temporaloptions.OptionActivityWaitForCancel, wait)
			start.Task = runtimeapi.Task{ID: registry.NewID("test", "activity"), Options: options}
			require.NoError(t, d.executeFunctionAsyncStart(start, 1))
			cancel := function.AcquireAsyncCancelCmd()
			defer cancel.Release()
			cancel.Topic = start.Topic
			require.NoError(t, d.executeFunctionAsyncCancel(cancel, 2))
			require.Equal(t, 1, env.cancellations)
			require.Equal(t, 1, messages, "workflow cancel must retain its immediate terminal")
			require.Empty(t, d.signals)
			require.NotContains(t, d.asyncActivities, start.Topic)
			require.Equal(t, 2, acknowledgements, "cancel must acknowledge without waiting for the activity")
			require.Equal(t, wait, env.activities[0].WaitForCancellation, "configured cancellation policy was overridden")
			wire, err := d.dc.ToPayloads(payload.Payloads{jsonLiteral("completed")})
			require.NoError(t, err)
			env.activityDone[0](wire, nil)
			env.activityDone[0](nil, temporal.NewCanceledError())
			require.Empty(t, d.signals, "late outcomes must not reopen a canceled workflow future")
		})
	}
}
