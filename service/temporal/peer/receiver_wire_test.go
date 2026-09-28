// SPDX-License-Identifier: MPL-2.0

package peer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/payload"
	"go.uber.org/zap"
)

// A monitor request from another node crosses the internode codec before it
// reaches the receiver hosting the workflow.
func TestReceiver_CrossNodeMonitorRequestReachesTypedHandler(t *testing.T) {
	r := NewReceiver(context.Background(), "temporal-client", nil, &mockRouter{}, zap.NewNop())
	t.Cleanup(r.Stop)

	workflowPID := pid.PID{Node: "temporal-client", Host: "task-queue", UniqID: "workflow-wire"}
	remoteCaller := pid.PID{Node: "node-a", Host: "host1", UniqID: "process-1"}

	// An active watch keeps the handler from starting a Temporal poll.
	r.mu.Lock()
	r.watchers[workflowPID.UniqID] = &workflowWatcher{
		workflowID: workflowPID.UniqID,
		taskQueue:  workflowPID.Host,
		monitors:   make(map[string]pid.PID),
		links:      make(map[string]pid.PID),
		watching:   true,
	}
	r.mu.Unlock()

	codec := internode.NewMessageCodec(payload.NewTranscoder())
	data, err := codec.Encode(topology.MonitorRequestPackage(remoteCaller, workflowPID))
	require.NoError(t, err)
	decoded, err := codec.Decode(data)
	require.NoError(t, err)
	defer relay.ReleasePackage(decoded)

	require.NoError(t, r.Send(decoded))

	r.mu.RLock()
	defer r.mu.RUnlock()
	monitors := r.watchers[workflowPID.UniqID].monitors
	require.Len(t, monitors, 1)
	got, ok := monitors[remoteCaller.String()]
	require.True(t, ok)
	assert.Equal(t, remoteCaller.String(), got.String())
}
