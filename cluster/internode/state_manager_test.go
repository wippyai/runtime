// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"net"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func setupStateManager() *NodeStateManager {
	config := DefaultManagerConfig()
	config.Logger = zap.NewNop()
	logger := zap.NewNop()
	return NewNodeStateManager(config, newTelemetry(nil), logger)
}

// setupStateManagerSmallCaps builds a manager whose per-class caps are
// small so overflow can be driven deterministically in a unit test.
func setupStateManagerSmallCaps(cap int) *NodeStateManager {
	config := DefaultManagerConfig()
	config.Logger = zap.NewNop()
	config.RaftControlQueueCap = cap
	config.GossipQueueCap = cap
	config.PGBroadcastQueueCap = cap
	config.RaftMeshQueueCap = cap
	return NewNodeStateManager(config, newTelemetry(nil), zap.NewNop())
}

// drainAllData drains every queued frame across all classes in QoS order
// and returns the raw payloads. Used to inspect what survived an overflow.
func drainAllData(nsm *NodeStateManager, nodeID cluster.NodeID) [][]byte {
	out := nsm.DrainMessages(nodeID, 1<<20)
	data := make([][]byte, 0, len(out))
	for _, o := range out {
		data = append(data, o.Data)
	}
	return data
}

func createMockConnection(nodeID cluster.NodeID) *NodeConnection {
	client, server := net.Pipe()
	defer func() { _ = server.Close() }()

	config := DefaultNodeConnectionConfig()
	logger := zap.NewNop()
	return newNodeConnection(client, nodeID, config, logger)
}

func TestNodeStateManager_CreateNodeState(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	// Create new node state
	nsm.CreateNodeState(nodeID)

	// Verify state exists
	state := nsm.GetNodeState(nodeID)
	require.NotNil(t, state)
	for i, q := range state.queues {
		assert.NotNil(t, q, "queue[%d] must not be nil", i)
	}
	assert.NotNil(t, state.messageNotify)
	assert.Equal(t, StateNone, state.state)
}

func TestNodeStateManager_CreateNodeState_Duplicate(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	// Create twice
	nsm.CreateNodeState(nodeID)
	nsm.CreateNodeState(nodeID) // Should be idempotent

	// Should still work
	state := nsm.GetNodeState(nodeID)
	assert.NotNil(t, state)
}

func TestNodeStateManager_GetNodeState_NonExistent(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "non-existent"

	state := nsm.GetNodeState(nodeID)
	assert.Nil(t, state)
}

func TestNodeStateManager_RemoveNodeState(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	// Create and verify
	nsm.CreateNodeState(nodeID)
	assert.NotNil(t, nsm.GetNodeState(nodeID))

	// Remove
	nsm.RemoveNodeState(nodeID)

	// Verify removed
	assert.Nil(t, nsm.GetNodeState(nodeID))
}

func TestNodeStateManager_RemoveNodeState_WithConnection(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Set a mock connection
	mockConn := createMockConnection(nodeID)
	nsm.SetNodeConnection(nodeID, mockConn, StateConnected)

	// Remove should close connection
	nsm.RemoveNodeState(nodeID)

	// Verify removed
	assert.Nil(t, nsm.GetNodeState(nodeID))
}

func TestNodeStateManager_RemoveNodeState_NonExistent(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "non-existent"

	// Should not panic
	nsm.RemoveNodeState(nodeID)
}

func TestNodeStateManager_QueueMessage(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Queue message
	data := []byte("test message")
	err := nsm.QueueMessageClass(nodeID, data, ClassRaftControl)
	require.NoError(t, err)

	// Verify notification sent
	select {
	case <-nsm.GetMessageNotifier(nodeID):
		// Good
	default:
		t.Error("Expected message notification")
	}

	// Drain and verify
	messages := nsm.DrainMessages(nodeID, 10)
	require.Len(t, messages, 1)
	assert.Equal(t, data, messages[0].Data)
	assert.Equal(t, ClassRaftControl, messages[0].Class)
}

func TestNodeStateManager_QueueMessage_Nil(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Queue nil should be no-op
	err := nsm.QueueMessageClass(nodeID, nil, ClassRaftControl)
	require.NoError(t, err)

	messages := nsm.DrainMessages(nodeID, 10)
	assert.Len(t, messages, 0)
}

func TestNodeStateManager_QueueMessage_UnmanagedNode(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	// Should return error
	err := nsm.QueueMessageClass(nodeID, []byte("test"), ClassRaftControl)
	assert.Equal(t, ErrNodeNotManaged, err)
}

func TestNodeStateManager_QueueMessage_Multiple(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Queue multiple messages
	for i := 0; i < 5; i++ {
		data := []byte{byte(i)}
		err := nsm.QueueMessageClass(nodeID, data, ClassRaftControl)
		require.NoError(t, err)
	}

	// Drain and verify order
	messages := nsm.DrainMessages(nodeID, 10)
	require.Len(t, messages, 5)
	for i := 0; i < 5; i++ {
		assert.Equal(t, []byte{byte(i)}, messages[i].Data)
		assert.Equal(t, ClassRaftControl, messages[i].Class)
	}
}

func TestNodeStateManager_DrainMessages_MaxCount(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Queue 10 messages
	for i := 0; i < 10; i++ {
		_ = nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassRaftControl)
	}

	// Drain only 3
	messages := nsm.DrainMessages(nodeID, 3)
	assert.Len(t, messages, 3)

	// Remaining 7 should still be queued
	messages = nsm.DrainMessages(nodeID, 10)
	assert.Len(t, messages, 7)
}

func TestNodeStateManager_DrainMessages_Empty(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	messages := nsm.DrainMessages(nodeID, 10)
	assert.Len(t, messages, 0)
}

func TestNodeStateManager_DrainMessages_UnmanagedNode(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	messages := nsm.DrainMessages(nodeID, 10)
	assert.Nil(t, messages)
}

func TestNodeStateManager_DrainMessages_ZeroMaxCount(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)
	_ = nsm.QueueMessageClass(nodeID, []byte("test"), ClassRaftControl)

	messages := nsm.DrainMessages(nodeID, 0)
	assert.Nil(t, messages)
}

func TestNodeStateManager_RequeueMessages(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Queue initial message
	_ = nsm.QueueMessageClass(nodeID, []byte{1}, ClassRaftControl)

	// Requeue messages (should be inserted at front)
	toRequeue := [][]byte{{2}, {3}}
	nsm.RequeueMessagesClass(nodeID, toRequeue, ClassRaftControl)

	// Drain should return requeued messages first, then original
	messages := nsm.DrainMessages(nodeID, 10)
	require.Len(t, messages, 3)
	assert.Equal(t, []byte{2}, messages[0].Data) // First requeued
	assert.Equal(t, []byte{3}, messages[1].Data) // Second requeued
	assert.Equal(t, []byte{1}, messages[2].Data) // Original message last
}

func TestNodeStateManager_RequeueMessages_Empty(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Should be no-op
	nsm.RequeueMessagesClass(nodeID, [][]byte{}, ClassRaftControl)
}

func TestNodeStateManager_RequeueMessages_UnmanagedNode(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	// Should not panic, messages dropped
	nsm.RequeueMessagesClass(nodeID, [][]byte{{1}, {2}}, ClassRaftControl)
}

func TestNodeStateManager_SetGetNodeConnection(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Set connection
	mockConn := &NodeConnection{}
	nsm.SetNodeConnection(nodeID, mockConn, StateConnected)

	// Get connection
	conn, state := nsm.GetNodeConnection(nodeID)
	assert.Equal(t, mockConn, conn)
	assert.Equal(t, StateConnected, state)
}

func TestNodeStateManager_GetNodeConnection_NonExistent(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "non-existent"

	conn, state := nsm.GetNodeConnection(nodeID)
	assert.Nil(t, conn)
	assert.Equal(t, StateNone, state)
}

func TestNodeStateManager_SetNodeConnection_UnmanagedNode(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	// Should not panic
	mockConn := &NodeConnection{}
	nsm.SetNodeConnection(nodeID, mockConn, StateConnected)
}

func TestNodeStateManager_SetGetNodeState(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Set state
	nsm.SetNodeState(nodeID, StateRetrying)

	// Get state via GetNodeConnection
	_, state := nsm.GetNodeConnection(nodeID)
	assert.Equal(t, StateRetrying, state)
}

func TestNodeStateManager_SetNodeState_UnmanagedNode(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	// Should not panic
	nsm.SetNodeState(nodeID, StateConnected)
}

func TestNodeStateManager_UpdateGetNodeAddress(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Update address
	nsm.UpdateNodeAddress(nodeID, "192.168.1.1", 8080)

	// Get address
	addr, port, ok := nsm.GetNodeAddress(nodeID)
	assert.True(t, ok)
	assert.Equal(t, "192.168.1.1", addr)
	assert.Equal(t, 8080, port)
}

func TestNodeStateManager_GetNodeAddress_NonExistent(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "non-existent"

	addr, port, ok := nsm.GetNodeAddress(nodeID)
	assert.False(t, ok)
	assert.Equal(t, "", addr)
	assert.Equal(t, 0, port)
}

func TestNodeStateManager_GetNodeAddress_NotSet(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	// Address not set yet
	addr, port, ok := nsm.GetNodeAddress(nodeID)
	assert.False(t, ok)
	assert.Equal(t, "", addr)
	assert.Equal(t, 0, port)
}

func TestNodeStateManager_UpdateNodeAddress_UnmanagedNode(_ *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	// Should not panic
	nsm.UpdateNodeAddress(nodeID, "192.168.1.1", 8080)
}

func TestNodeStateManager_GetMessageNotifier(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"

	nsm.CreateNodeState(nodeID)

	notifier := nsm.GetMessageNotifier(nodeID)
	assert.NotNil(t, notifier)

	// Queue message should trigger notifier
	_ = nsm.QueueMessageClass(nodeID, []byte("test"), ClassRaftControl)

	select {
	case <-notifier:
		// Good
	default:
		t.Error("Expected notification")
	}
}

func TestNodeStateManager_GetMessageNotifier_UnmanagedNode(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "unmanaged"

	notifier := nsm.GetMessageNotifier(nodeID)
	assert.Nil(t, notifier)
}

func TestNodeStateManager_GetConnectedNodes_Empty(t *testing.T) {
	nsm := setupStateManager()

	connected := nsm.GetConnectedNodes()
	assert.Len(t, connected, 0)
}

func TestNodeStateManager_GetConnectedNodes(t *testing.T) {
	nsm := setupStateManager()

	// Create multiple nodes with different states
	node1 := "node-1"
	node2 := "node-2"
	node3 := "node-3"

	nsm.CreateNodeState(node1)
	nsm.CreateNodeState(node2)
	nsm.CreateNodeState(node3)

	// Set different states
	nsm.SetNodeState(node1, StateConnected)
	nsm.SetNodeState(node2, StateRetrying)
	nsm.SetNodeState(node3, StateConnected)

	// Only connected nodes should be returned
	connected := nsm.GetConnectedNodes()
	assert.Len(t, connected, 2)
	assert.Contains(t, connected, node1)
	assert.Contains(t, connected, node3)
	assert.NotContains(t, connected, node2)
}

func TestNodeStateManager_Concurrent_QueueDrain(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"
	nsm.CreateNodeState(nodeID)

	const numGoroutines = 10
	const messagesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Queuers + Drainers

	// Concurrent queueing
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				data := []byte{byte(id), byte(j)}
				_ = nsm.QueueMessageClass(nodeID, data, ClassRaftControl)
			}
		}(i)
	}

	// Concurrent draining
	totalDrained := 0
	var drainMu sync.Mutex
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine/10; j++ {
				messages := nsm.DrainMessages(nodeID, 10)
				drainMu.Lock()
				totalDrained += len(messages)
				drainMu.Unlock()
			}
		}()
	}

	wg.Wait()

	// Drain remaining
	remaining := nsm.DrainMessages(nodeID, 10000)
	totalDrained += len(remaining)

	// Verify we got all messages
	assert.Equal(t, numGoroutines*messagesPerGoroutine, totalDrained)
}

func TestNodeStateManager_Concurrent_StateUpdates(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"
	nsm.CreateNodeState(nodeID)

	const numGoroutines = 50
	states := []ConnectionState{StateConnecting, StateConnected, StateRetrying, StateDead}

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Writers + Readers

	// Concurrent state writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			state := states[id%len(states)]
			nsm.SetNodeState(nodeID, state)
		}(i)
	}

	// Concurrent state reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, state := nsm.GetNodeConnection(nodeID)
			// Just verify it's a valid state
			assert.GreaterOrEqual(t, int(state), int(StateNone))
			assert.LessOrEqual(t, int(state), int(StateDead))
		}()
	}

	wg.Wait()
}

func TestNodeStateManager_Concurrent_CreateRemove(_ *testing.T) {
	nsm := setupStateManager()

	const numGoroutines = 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// Concurrent creates
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			nodeID := "test-node-" + strconv.Itoa(id)
			nsm.CreateNodeState(nodeID)
			_ = nsm.QueueMessageClass(nodeID, []byte{byte(id)}, ClassRaftControl)
		}(i)
	}

	// Concurrent removes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			nodeID := "test-node-" + strconv.Itoa(id)
			// May or may not exist yet, should not panic
			nsm.RemoveNodeState(nodeID)
		}(i)
	}

	wg.Wait()
}

// TestQueueMessageClass_RaftMeshOverflow_ResetsNotDropsMidStream proves
// the byte-stream contract for ClassRaftMesh. ClassRaftMesh carries a
// yamux-multiplexed byte-stream, so dropping a frame from the middle
// desyncs the demuxer and wedges the session. On overflow the queue must
// signal a session reset (ErrRaftMeshOverflow) and clear the pending
// stream — never drop-oldest and hand a gap-riddled stream downstream.
func TestQueueMessageClass_RaftMeshOverflow_ResetsNotDropsMidStream(t *testing.T) {
	const cap = 4
	nsm := setupStateManagerSmallCaps(cap)
	nodeID := "peer-1"
	nsm.CreateNodeState(nodeID)

	// Fill the queue exactly to capacity with ordered byte-stream frames.
	for i := 0; i < cap; i++ {
		require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassRaftMesh))
	}

	// The next frame overflows. A drop-oldest queue would silently discard
	// frame 0 and accept frame cap, leaving a stream with a hole in the
	// middle. The byte-stream-safe contract: return ErrRaftMeshOverflow and
	// discard the whole pending stream so the session can be reset and
	// resynced from a consistent point.
	err := nsm.QueueMessageClass(nodeID, []byte{byte(cap)}, ClassRaftMesh)
	require.ErrorIs(t, err, ErrRaftMeshOverflow,
		"overflow must signal a session reset, not drop-oldest")

	// No surviving stream with a mid-stream gap: the queue is cleared.
	survived := drainAllData(nsm, nodeID)
	require.Empty(t, survived,
		"on overflow the pending byte-stream must be discarded wholesale, not partially")
}

// TestQueueMessageClass_RaftMeshNoOverflow_PreservesOrder asserts that
// below capacity ClassRaftMesh keeps every frame in strict order — the
// byte-stream is delivered intact when there is room.
func TestQueueMessageClass_RaftMeshNoOverflow_PreservesOrder(t *testing.T) {
	const cap = 8
	nsm := setupStateManagerSmallCaps(cap)
	nodeID := "peer-1"
	nsm.CreateNodeState(nodeID)

	for i := 0; i < cap; i++ {
		require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassRaftMesh))
	}

	got := drainAllData(nsm, nodeID)
	require.Len(t, got, cap)
	for i := 0; i < cap; i++ {
		assert.Equal(t, []byte{byte(i)}, got[i], "frame %d out of order", i)
	}
}

// TestQueueMessageClass_RaftControlDropOldestUnchanged guards the datagram
// class behavior: ClassRaftControl still drops the OLDEST entry on
// overflow (etcd semantics) and accepts the new frame. No regression from
// the byte-stream fix.
func TestQueueMessageClass_RaftControlDropOldestUnchanged(t *testing.T) {
	const cap = 4
	nsm := setupStateManagerSmallCaps(cap)
	nodeID := "peer-1"
	nsm.CreateNodeState(nodeID)

	for i := 0; i < cap; i++ {
		require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassRaftControl))
	}
	// Overflow: drop-oldest, accept new, no error.
	require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(cap)}, ClassRaftControl))

	got := drainAllData(nsm, nodeID)
	require.Len(t, got, cap, "drop-oldest keeps the queue at capacity")
	// Oldest (frame 0) dropped; frames 1..cap survive in order.
	for i := 0; i < cap; i++ {
		assert.Equal(t, []byte{byte(i + 1)}, got[i])
	}
}

// TestQueueMessageClass_GossipDropNewestUnchanged guards the drop-newest
// datagram classes: ClassGossip rejects the new frame with ErrQueueFull on
// overflow and keeps the existing entries. No regression.
func TestQueueMessageClass_GossipDropNewestUnchanged(t *testing.T) {
	const cap = 4
	nsm := setupStateManagerSmallCaps(cap)
	nodeID := "peer-1"
	nsm.CreateNodeState(nodeID)

	for i := 0; i < cap; i++ {
		require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassGossip))
	}
	err := nsm.QueueMessageClass(nodeID, []byte{byte(cap)}, ClassGossip)
	require.ErrorIs(t, err, ErrQueueFull, "drop-newest classes reject the new frame")

	got := drainAllData(nsm, nodeID)
	require.Len(t, got, cap)
	for i := 0; i < cap; i++ {
		assert.Equal(t, []byte{byte(i)}, got[i], "existing entries kept on drop-newest")
	}
}

// TestQueueMessageClass_PGBroadcastDropNewestUnchanged mirrors the gossip
// guard for ClassPGBroadcast.
func TestQueueMessageClass_PGBroadcastDropNewestUnchanged(t *testing.T) {
	const cap = 4
	nsm := setupStateManagerSmallCaps(cap)
	nodeID := "peer-1"
	nsm.CreateNodeState(nodeID)

	for i := 0; i < cap; i++ {
		require.NoError(t, nsm.QueueMessageClass(nodeID, []byte{byte(i)}, ClassPGBroadcast))
	}
	err := nsm.QueueMessageClass(nodeID, []byte{byte(cap)}, ClassPGBroadcast)
	require.ErrorIs(t, err, ErrQueueFull)

	got := drainAllData(nsm, nodeID)
	require.Len(t, got, cap)
}

func TestNodeStateManager_Concurrent_AddressUpdates(t *testing.T) {
	nsm := setupStateManager()
	nodeID := "test-node-1"
	nsm.CreateNodeState(nodeID)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // Writers + Readers

	// Concurrent address writes
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			addr := "192.168.1." + strconv.Itoa(id%10)
			port := 8000 + id
			nsm.UpdateNodeAddress(nodeID, addr, port)
		}(i)
	}

	// Concurrent address reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			addr, port, ok := nsm.GetNodeAddress(nodeID)
			// If ok is true, verify valid data
			if ok {
				assert.NotEmpty(t, addr)
				assert.Greater(t, port, 0)
			}
		}()
	}

	wg.Wait()
}
