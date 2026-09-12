// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func boundedReliableStates() *NodeStateManager {
	config := DefaultManagerConfig()
	config.OutboundQueueSize = 1
	// This fixture deliberately exercises a single shared capacity without reserves.
	config.OutboundControlPeerEntries, config.OutboundControlPeerBytes = 0, 0
	config.OutboundControlTotalEntries, config.OutboundControlTotalBytes = 0, 0
	config.OutboundPeerBytes = 8
	config.OutboundTotalEntries = 2
	config.OutboundTotalBytes = 16
	return NewNodeStateManager(config, newTelemetry(nil), zap.NewNop())
}

func TestReliableAdmissionRetainsCreditAcrossWriterRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := boundedReliableStates()
		states.CreateNodeState("peer")
		require.NoError(t, states.QueueMessageClass("peer", []byte("12345678"), ClassRaftRPC))
		flight := states.DrainMessages("peer", 1)
		require.ErrorIs(t, states.QueueMessageClass("peer", []byte("x"), ClassPGBroadcast), ErrQueueFull)
		done := make(chan error, 1)
		go func() {
			done <- states.QueueMessageClassContext(context.Background(), "peer", []byte("next"), ClassRaftRPC)
		}()
		synctest.Wait()
		states.RequeueMessages("peer", flight)
		select {
		case err := <-done:
			t.Fatalf("retry prematurely freed credit: %v", err)
		default:
		}
		flight = states.DrainMessages("peer", 1)
		releaseOutbound(flight)
		require.NoError(t, <-done)
		next := states.DrainMessages("peer", 1)
		require.Equal(t, []byte("next"), next[0].Data)
		releaseOutbound(next)
		require.Zero(t, states.reliable.bytes)
	})
}

func TestReliableAdmissionRemovalCancelsCapacityWaiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := boundedReliableStates()
		states.CreateNodeState("peer")
		require.NoError(t, states.QueueMessageClass("peer", []byte("12345678"), ClassRaftControl))
		flight := states.DrainMessages("peer", 1)
		done := make(chan error, 1)
		go func() {
			done <- states.QueueMessageClassContext(context.Background(), "peer", []byte("next"), ClassRaftRPC)
		}()
		synctest.Wait()
		states.RemoveNodeState("peer")
		require.ErrorIs(t, <-done, ErrNodeNotManaged)
		require.Equal(t, uint64(8), states.reliable.bytes)
		releaseOutbound(flight)
		require.Zero(t, states.reliable.bytes)
	})
}

func TestReliableAdmissionAggregateBoundsAcrossPeers(t *testing.T) {
	states := boundedReliableStates()
	for _, peer := range []string{"a", "b", "c"} {
		states.CreateNodeState(peer)
	}
	require.NoError(t, states.QueueMessageClass("a", []byte("12345678"), ClassRaftRPC))
	require.NoError(t, states.QueueMessageClass("b", []byte("12345678"), ClassPGBroadcast))
	require.ErrorIs(t, states.QueueMessageClass("c", []byte("x"), ClassRaftControl), ErrQueueFull)
	require.Error(t, states.QueueMessageClass("c", []byte("123456789"), ClassRaftRPC))
	releaseOutbound(states.DrainMessages("a", 1))
	require.NoError(t, states.QueueMessageClass("c", []byte("x"), ClassRaftControl))
	states.RemoveNodeState("b")
	states.RemoveNodeState("c")
	require.Zero(t, states.reliable.entries)
	require.Zero(t, states.reliable.bytes)
}

func TestReliableAdmissionResetCancelsOldWaiterWithoutCreditingWriter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := boundedReliableStates()
		states.CreateNodeState("peer")
		old := states.GetNodeState("peer")
		require.NoError(t, states.QueueMessageClass("peer", []byte("12345678"), ClassRaftRPC))
		flight := states.DrainMessages("peer", 1)
		oldDone := make(chan error, 1)
		go func() {
			oldDone <- states.QueueMessageClassContext(context.Background(), "peer", []byte("old"), ClassRaftRPC)
		}()
		synctest.Wait()
		states.CreateNodeState("peer")
		require.Same(t, old, states.GetNodeState("peer"))
		require.ErrorIs(t, <-oldDone, ErrNodeNotManaged)
		require.Equal(t, uint64(8), states.reliable.bytes)
		newDone := make(chan error, 1)
		go func() {
			newDone <- states.QueueMessageClassContext(context.Background(), "peer", []byte("new"), ClassRaftRPC)
		}()
		synctest.Wait()
		select {
		case err := <-newDone:
			t.Fatalf("reset credited in-flight writer prematurely: %v", err)
		default:
		}
		// Old connection fails after reset: dispose its batch, never reinsert it.
		states.requeueMessagesForState("peer", old, flight)
		require.NoError(t, <-newDone)
		next := states.DrainMessages("peer", 2)
		require.Len(t, next, 1)
		require.Equal(t, []byte("new"), next[0].Data)
		releaseOutbound(next)
		require.Zero(t, states.reliable.bytes)
	})
}

func TestReliableAdmissionManagerStopCancelsWaiterAndDisposesQueues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := boundedReliableStates()
		for _, peer := range []string{"writer", "queued", "waiting"} {
			states.CreateNodeState(peer)
		}
		require.NoError(t, states.QueueMessageClass("writer", []byte("12345678"), ClassRaftRPC))
		flight := states.DrainMessages("writer", 1)
		require.NoError(t, states.QueueMessageClass("queued", []byte("12345678"), ClassPGBroadcast))
		manager := &manager{nodeStates: states, logger: zap.NewNop()}
		done := make(chan error, 1)
		go func() {
			done <- manager.SendToNodeContext(context.Background(), "waiting", []byte("next"), ClassRaftControl)
		}()
		synctest.Wait()
		require.NoError(t, manager.Stop())
		require.ErrorIs(t, <-done, ErrNodeNotManaged)
		require.Nil(t, states.GetNodeState("queued"))
		require.Equal(t, uint64(8), states.reliable.bytes, "only writer retains ownership after queue cleanup")
		releaseOutbound(flight)
		require.Zero(t, states.reliable.bytes)
		states.CreateNodeState("late")
		require.ErrorIs(t, manager.SendToNode("late", []byte("late"), ClassRaftRPC), ErrNodeNotManaged)
		require.NoError(t, manager.Stop())
	})
}
