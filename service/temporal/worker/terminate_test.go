// SPDX-License-Identifier: MPL-2.0

package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"go.temporal.io/sdk/activity"
	sdkworker "go.temporal.io/sdk/worker"
)

type systemActivityWorker struct {
	sdkworker.Worker
	name    string
	handler any
}

func (w *systemActivityWorker) RegisterActivityWithOptions(handler any, options activity.RegisterOptions) {
	w.name, w.handler = options.Name, handler
}

func TestTerminationActivityCallsTemporalTerminationAndReturnsFailure(t *testing.T) {
	sdk := &systemActivityWorker{}
	failure := errors.New("termination denied")
	called := 0
	client := &testTemporalClient{terminateFn: func(ctx context.Context, workflowID, runID, reason string, details ...any) error {
		called++
		require.Equal(t, "target", workflowID)
		require.Empty(t, runID)
		require.Equal(t, "process.terminate", reason)
		require.NoError(t, ctx.Err())
		return failure
	}}
	w := &Worker{}
	w.registerTerminateWorkflowActivity(&workerRuntime{worker: sdk, temporalClient: client})
	require.Equal(t, temporalapi.TerminateWorkflowActivityName, sdk.name)
	handler := sdk.handler.(func(context.Context, string) error)
	require.ErrorIs(t, handler(context.Background(), "target"), failure)
	require.Equal(t, 1, called)
	failure = nil
	require.NoError(t, handler(context.Background(), "target"))
	require.Equal(t, 2, called)
}
