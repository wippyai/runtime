// SPDX-License-Identifier: MPL-2.0

package workflow_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/service/temporal/dataconverter"
	"github.com/wippyai/runtime/service/temporal/workflow"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/temporalproto"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	sdkworkflow "go.temporal.io/sdk/workflow"
)

// TestReplay_Integration runs a Lua workflow, then replays its history against the
// same DefinitionFactory. A nil error proves deterministic replay.
func TestReplay_Integration(t *testing.T) {
	wfID := registry.NewID("test.workflow", "replay-hello")
	f := newWorkflowTestFixture(t, workflowTestOpts{
		workflowID: wfID,
		source:     helloWorkflowSource,
		taskQueue:  "test-replay-queue",
	})
	defer f.cleanup()

	run := f.startWorkflow(wfID.String(), map[string]any{"name": "Replay"})
	var result map[string]any
	require.NoError(t, run.Get(f.ctx, &result))

	hist := fetchHistory(t, f.ctx, f.temporalClient, run.GetID(), run.GetRunID())
	require.NotEmpty(t, hist.Events)

	dc := dataconverter.NewDataConverter(newTestTranscoder())
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{DataConverter: dc})
	require.NoError(t, err)

	factory := (&workflow.DefinitionFactory{ID: wfID}).WithContext(f.ctx)
	replayer.RegisterWorkflowWithOptions(factory, sdkworkflow.RegisterOptions{Name: wfID.String()})

	require.NoError(t, replayer.ReplayWorkflowHistory(nil, hist))

	// Cover the module's exact path: history written to a JSON file, replayed with a nil logger.
	data, err := temporalproto.CustomJSONMarshalOptions{}.Marshal(hist)
	require.NoError(t, err)
	histFile := filepath.Join(t.TempDir(), "history.json")
	require.NoError(t, os.WriteFile(histFile, data, 0o600))
	require.NoError(t, replayer.ReplayWorkflowHistoryFromJSONFile(nil, histFile))
}

func fetchHistory(t *testing.T, ctx context.Context, c client.Client, wfID, runID string) *historypb.History {
	t.Helper()
	it := c.GetWorkflowHistory(ctx, wfID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	var events []*historypb.HistoryEvent
	for it.HasNext() {
		ev, err := it.Next()
		require.NoError(t, err)
		events = append(events, ev)
	}
	return &historypb.History{Events: events}
}
