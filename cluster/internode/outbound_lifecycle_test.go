// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOutboundReservationFollowsDrainRetryAndFlush(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	budget, err := newOutboundBudget(1, 8)
	require.NoError(t, err)
	reservation, err := budget.reserve(context.Background(), 8, false)
	require.NoError(t, err)
	state.queueMu.Lock()
	require.True(t, state.queues[ClassRaftControl].pushFrame(Outbound{Data: []byte("retained"), Class: ClassRaftControl, reservation: reservation}))
	state.queueMu.Unlock()
	first := states.DrainMessages("peer", 1)
	require.Len(t, first, 1)
	_, err = budget.reserve(context.Background(), 1, false)
	require.ErrorIs(t, err, ErrQueueFull, "draining cannot free retained credit")
	states.RequeueMessages("peer", first)
	second := states.DrainMessages("peer", 1)
	require.Len(t, second, 1)
	require.Same(t, reservation, second[0].reservation)
	require.Equal(t, first[0].Data, second[0].Data)
	require.Equal(t, uint64(8), budget.bytes)
	releaseOutbound(second)
	require.Zero(t, budget.bytes)
	require.Zero(t, budget.entries)
}

func TestOutboundPeerRemovalDisposesQueuedAndLateWriterReservations(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	old := states.GetNodeState("peer")
	budget, err := newOutboundBudget(3, 24)
	require.NoError(t, err)
	for range 2 {
		reservation, err := budget.reserve(context.Background(), 8, false)
		require.NoError(t, err)
		old.queueMu.Lock()
		old.queues[ClassRaftControl].pushFrame(Outbound{Data: []byte("retained"), reservation: reservation})
		old.queueMu.Unlock()
	}
	flight := states.DrainMessages("peer", 1)
	states.RemoveNodeState("peer")
	require.Equal(t, uint64(1), budget.entries, "in-flight writer still owns its bytes")
	states.CreateNodeState("peer")
	replacement, err := budget.reserve(context.Background(), 8, false)
	require.NoError(t, err)
	states.requeueMessagesForState("peer", old, flight)
	require.Equal(t, uint64(1), budget.entries, "stale requeue disposes only its old reservation")
	require.Empty(t, states.DrainMessages("peer", 8), "old frames cannot repopulate replacement")
	releaseOutbound(flight)
	require.Equal(t, uint64(1), budget.entries, "late duplicate completion cannot credit replacement")
	replacement.release()
	require.Zero(t, budget.bytes)
}

func TestOutboundResetFencesOldDrainAndRequeue(t *testing.T) {
	states := setupStateManager()
	states.CreateNodeState("peer")
	state := states.GetNodeState("peer")
	oldGeneration := state.generation
	budget, err := newOutboundBudget(2, 16)
	require.NoError(t, err)
	reservation, err := budget.reserve(context.Background(), 8, false)
	require.NoError(t, err)
	state.queues[ClassRaftControl].pushFrame(Outbound{Data: []byte("old-data"), reservation: reservation})
	flight := states.DrainMessages("peer", 1)
	require.Len(t, flight, 1)
	states.CreateNodeState("peer")
	require.Same(t, state, states.GetNodeState("peer"), "reset preserves control-loop state pointer")
	require.NoError(t, states.QueueMessageClass("peer", []byte("new-data"), ClassRaftControl))
	require.Empty(t, states.drainMessagesForGeneration("peer", state, oldGeneration, 1), "old writer cannot consume replacement queue")
	states.requeueMessagesForState("peer", state, flight)
	require.Zero(t, budget.bytes, "stale writer disposition releases retained old bytes")
	next := states.DrainMessages("peer", 2)
	require.Len(t, next, 1)
	require.Equal(t, []byte("new-data"), next[0].Data)
}
