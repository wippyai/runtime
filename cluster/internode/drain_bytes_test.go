// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDrainByteBoundReconsidersControlBetweenBulkBatches(t *testing.T) {
	config := DefaultManagerConfig()
	config.DrainBatchBytes = 8
	states := NewNodeStateManager(config, newTelemetry(nil), zap.NewNop())
	states.CreateNodeState("peer")
	for _, data := range []string{"aaaa", "bbbb", "cccc"} {
		require.NoError(t, states.QueueMessageClass("peer", []byte(data), ClassPGBroadcast))
	}
	first := states.DrainMessages("peer", 32)
	require.Len(t, first, 2)
	require.Equal(t, []byte("aaaa"), first[0].Data)
	require.Equal(t, []byte("bbbb"), first[1].Data)
	require.NoError(t, states.QueueMessageClass("peer", []byte("ctrl"), ClassRaftControl))
	next := states.DrainMessages("peer", 32)
	require.Len(t, next, 2)
	require.Equal(t, ClassRaftControl, next[0].Class)
	require.Equal(t, []byte("cccc"), next[1].Data)
	releaseOutbound(first)
	releaseOutbound(next)
	require.Zero(t, states.reliable.bytes)
}

func TestDrainByteTargetDoesNotStrandLargeAcceptedFrame(t *testing.T) {
	config := DefaultManagerConfig()
	config.DrainBatchBytes = 2
	states := NewNodeStateManager(config, newTelemetry(nil), zap.NewNop())
	states.CreateNodeState("peer")
	require.NoError(t, states.QueueMessageClass("peer", []byte("larger"), ClassRaftRPC))
	require.NoError(t, states.QueueMessageClass("peer", []byte("x"), ClassRaftRPC))
	first := states.DrainMessages("peer", 32)
	require.Len(t, first, 1)
	require.Equal(t, []byte("larger"), first[0].Data)
	states.RequeueMessages("peer", first)
	retry := states.DrainMessages("peer", 32)
	require.Len(t, retry, 1)
	require.Same(t, first[0].reservation, retry[0].reservation)
	releaseOutbound(retry)
	next := states.DrainMessages("peer", 32)
	require.Len(t, next, 1)
	require.Equal(t, []byte("x"), next[0].Data)
	releaseOutbound(next)
	require.Zero(t, states.reliable.bytes)
}
