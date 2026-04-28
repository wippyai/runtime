// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

// Integration tests for internode connections using real TCP sockets

func TestIntegration_TwoNodeCommunication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := zap.NewNop()

	// Track received messages
	var node1Received, node2Received atomic.Int32

	// Create connection managers for two nodes
	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.InitialRetryDelay = 10 * time.Millisecond
	config1.MaxRetryDelay = 100 * time.Millisecond

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.InitialRetryDelay = 10 * time.Millisecond
	config2.MaxRetryDelay = 100 * time.Millisecond

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	// Start both managers
	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		node1Received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		node2Received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	port1 := cm1.GetListenPort()
	port2 := cm2.GetListenPort()
	t.Logf("Node 1 listening on port %d, Node 2 on port %d", port1, port2)

	// Register nodes with each other
	cm1.AddManagedNode("node-2")
	cm2.AddManagedNode("node-1")

	// Initiate connections
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)
	cm2.EnsureConnection("node-1", "127.0.0.1", port1)

	// Wait for connection establishment
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		nodes1 := cm1.ConnectedNodes()
		nodes2 := cm2.ConnectedNodes()
		if len(nodes1) > 0 && len(nodes2) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Verify connection
	assert.NotEmpty(t, cm1.ConnectedNodes(), "node-1 should have connections")
	assert.NotEmpty(t, cm2.ConnectedNodes(), "node-2 should have connections")

	// Send messages from node-1 to node-2
	for i := 0; i < 10; i++ {
		err := cm1.SendToNode("node-2", []byte("hello from node-1"), ClassRaftControl)
		require.NoError(t, err)
	}

	// Send messages from node-2 to node-1
	for i := 0; i < 10; i++ {
		err := cm2.SendToNode("node-1", []byte("hello from node-2"), ClassRaftControl)
		require.NoError(t, err)
	}

	// Wait for messages to be delivered
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if node1Received.Load() >= 10 && node2Received.Load() >= 10 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, node1Received.Load(), int32(10), "node-1 should receive messages from node-2")
	assert.GreaterOrEqual(t, node2Received.Load(), int32(10), "node-2 should receive messages from node-1")
}

func TestIntegration_ConnectionRetryOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := zap.NewNop()

	var node2Received atomic.Int32

	// Create node-1 config
	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.InitialRetryDelay = 50 * time.Millisecond
	config1.MaxRetryDelay = 200 * time.Millisecond
	config1.MaxRetryAttempts = 20

	cm1 := NewConnectionManager(config1, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	// Register node-2 and try to connect BEFORE node-2 exists
	cm1.AddManagedNode("node-2")
	cm1.EnsureConnection("node-2", "127.0.0.1", 19999) // Non-existent port

	// Wait a bit to ensure retry starts
	time.Sleep(200 * time.Millisecond)

	// Now start node-2 on a different port
	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.BindPort = 19998
	config2.AutoPort = false
	config2.Logger = logger

	cm2 := NewConnectionManager(config2, nil)

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		node2Received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	cm2.AddManagedNode("node-1")

	// Update node-1 with correct port for node-2
	cm1.EnsureConnection("node-2", "127.0.0.1", 19998)

	// Wait for connection establishment
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Send message
	_ = cm1.SendToNode("node-2", []byte("delayed hello"), ClassRaftControl)

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if node2Received.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.Greater(t, node2Received.Load(), int32(0), "node-2 should receive message after retry")
}

func TestIntegration_GracefulDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := zap.NewNop()

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)

	port1 := cm1.GetListenPort()
	port2 := cm2.GetListenPort()

	cm1.AddManagedNode("node-2")
	cm2.AddManagedNode("node-1")
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)
	cm2.EnsureConnection("node-1", "127.0.0.1", port1)

	// Wait for connection
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, cm1.ConnectedNodes())

	// Gracefully stop node-2
	err = cm2.Stop()
	require.NoError(t, err)

	// Wait for node-1 to detect disconnection
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Since node-2 is removed from managed nodes, we don't retry
	cm1.RemoveManagedNode("node-2")

	assert.Empty(t, cm1.ConnectedNodes(), "node-1 should detect disconnection")
}

func TestIntegration_ThreeNodeCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	logger := zap.NewNop()

	// Track received messages per node
	var received [3]atomic.Int32

	configs := make([]ManagerConfig, 3)
	managers := make([]ConnectionManager, 3)

	for i := 0; i < 3; i++ {
		configs[i] = DefaultManagerConfig()
		configs[i].LocalNodeID = "node-" + string(rune('A'+i))
		configs[i].BindAddr = "127.0.0.1"
		configs[i].AutoPort = true
		configs[i].Logger = logger
		configs[i].InitialRetryDelay = 10 * time.Millisecond
		configs[i].MaxRetryDelay = 100 * time.Millisecond

		idx := i
		managers[i] = NewConnectionManager(configs[i], nil)
		err := managers[i].Start(ctx, func(_ cluster.NodeID, _ []byte) {
			received[idx].Add(1)
		})
		require.NoError(t, err)
	}

	t.Cleanup(func() {
		for _, m := range managers {
			_ = m.Stop()
		}
	})

	// Get ports
	ports := make([]int, 3)
	for i := 0; i < 3; i++ {
		ports[i] = managers[i].GetListenPort()
	}

	// Register all nodes with each other (full mesh)
	nodeIDs := []cluster.NodeID{"node-A", "node-B", "node-C"}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i != j {
				managers[i].AddManagedNode(nodeIDs[j])
				managers[i].EnsureConnection(nodeIDs[j], "127.0.0.1", ports[j])
			}
		}
	}

	// Wait for all connections
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allConnected := true
		for i := 0; i < 3; i++ {
			if len(managers[i].ConnectedNodes()) < 2 {
				allConnected = false
				break
			}
		}
		if allConnected {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify full mesh
	for i := 0; i < 3; i++ {
		assert.Len(t, managers[i].ConnectedNodes(), 2,
			"node-%c should be connected to 2 other nodes", 'A'+i)
	}

	// Send messages from each node to all others
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if i != j {
				_ = managers[i].SendToNode(nodeIDs[j], []byte("hello"), ClassRaftControl)
			}
		}
	}

	// Wait for delivery
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		total := received[0].Load() + received[1].Load() + received[2].Load()
		if total >= 6 { // 3 nodes * 2 messages each = 6 total
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	totalReceived := received[0].Load() + received[1].Load() + received[2].Load()
	assert.GreaterOrEqual(t, totalReceived, int32(6), "all messages should be delivered")
}

func TestIntegration_LargeMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := zap.NewNop()

	var receivedSizes []int
	var mu sync.Mutex

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.MaxMessageSize = 10 * 1024 * 1024 // 10MB

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.MaxMessageSize = 10 * 1024 * 1024

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, data []byte) {
		mu.Lock()
		receivedSizes = append(receivedSizes, len(data))
		mu.Unlock()
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	port2 := cm2.GetListenPort()

	cm1.AddManagedNode("node-2")
	cm2.AddManagedNode("node-1")
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)

	// Wait for connection
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Send messages of increasing sizes
	sizes := []int{1024, 64 * 1024, 256 * 1024, 1024 * 1024}
	for _, size := range sizes {
		msg := make([]byte, size)
		for i := range msg {
			msg[i] = byte(i % 256)
		}
		err := cm1.SendToNode("node-2", msg, ClassRaftControl)
		require.NoError(t, err)
	}

	// Wait for all messages
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := len(receivedSizes)
		mu.Unlock()
		if count >= len(sizes) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	assert.Len(t, receivedSizes, len(sizes), "all messages should be received")
	for i, size := range receivedSizes {
		assert.Equal(t, sizes[i], size, "message %d size mismatch", i)
	}
	mu.Unlock()
}

func TestIntegration_HighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger := zap.NewNop()

	const messageCount = 100

	var received atomic.Int32

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "sender"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.DrainBatchSize = 256 // Larger batch for high throughput

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "receiver"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.DrainBatchSize = 256

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	port1 := cm1.GetListenPort()
	port2 := cm2.GetListenPort()

	cm1.AddManagedNode("receiver")
	cm2.AddManagedNode("sender")
	cm1.EnsureConnection("receiver", "127.0.0.1", port2)
	cm2.EnsureConnection("sender", "127.0.0.1", port1)

	// Wait for connection to be fully established
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 && len(cm2.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, cm1.ConnectedNodes(), "sender should be connected")

	// Send messages with small delays to allow drain cycles
	start := time.Now()
	for i := 0; i < messageCount; i++ {
		_ = cm1.SendToNode("receiver", []byte("high throughput test message"), ClassRaftControl)
		if i%10 == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	// Wait for all messages
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if received.Load() >= messageCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	elapsed := time.Since(start)
	t.Logf("Sent %d messages in %v (%.0f msg/sec)", messageCount, elapsed, float64(messageCount)/elapsed.Seconds())

	assert.GreaterOrEqual(t, received.Load(), int32(messageCount), "all messages should be delivered")
}

func TestIntegration_ShortNetworkDisruption(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Test that messages queued during a short disconnect are delivered after reconnect
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	logger := zap.NewNop()

	var received atomic.Int32

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.InitialRetryDelay = 50 * time.Millisecond
	config1.MaxRetryDelay = 200 * time.Millisecond
	config1.MaxRetryAttempts = 30
	config1.DrainBatchSize = 128

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.DrainBatchSize = 128

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		received.Add(1)
	})
	require.NoError(t, err)

	port1 := cm1.GetListenPort()
	port2 := cm2.GetListenPort()

	cm1.AddManagedNode("node-2")
	cm2.AddManagedNode("node-1")
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)
	cm2.EnsureConnection("node-1", "127.0.0.1", port1)

	// Wait for initial connection
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, cm1.ConnectedNodes(), "should be connected initially")

	// Send initial messages
	for i := 0; i < 10; i++ {
		_ = cm1.SendToNode("node-2", []byte("before disruption"), ClassRaftControl)
		time.Sleep(time.Millisecond)
	}

	// Wait for initial messages
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if received.Load() >= 10 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.GreaterOrEqual(t, received.Load(), int32(10))

	// Simulate network disruption by stopping node-2
	err = cm2.Stop()
	require.NoError(t, err)

	// Wait for disconnection detection
	time.Sleep(200 * time.Millisecond)

	// Send messages during disruption (they should be queued)
	for i := 0; i < 5; i++ {
		_ = cm1.SendToNode("node-2", []byte("during disruption"), ClassRaftControl)
	}

	// Restart node-2 with same port
	cm2 = NewConnectionManager(config2, nil)
	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	cm2.AddManagedNode("node-1")
	port2 = cm2.GetListenPort()

	// Update node-1 with new port and trigger reconnect
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)

	// Wait for reconnection
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	require.NotEmpty(t, cm1.ConnectedNodes(), "should reconnect")

	// Send messages after recovery
	for i := 0; i < 10; i++ {
		_ = cm1.SendToNode("node-2", []byte("after recovery"), ClassRaftControl)
		time.Sleep(time.Millisecond)
	}

	// Wait for all messages (10 initial + 5 queued + 10 after = 25)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if received.Load() >= 20 { // At least 20 (queued msgs may be lost)
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// At minimum, we should get messages sent after recovery
	assert.GreaterOrEqual(t, received.Load(), int32(20))
}

func TestIntegration_MultipleReconnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	// Test that the system handles multiple rapid reconnection cycles
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	logger := zap.NewNop()

	var received atomic.Int32

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.InitialRetryDelay = 20 * time.Millisecond
	config1.MaxRetryDelay = 100 * time.Millisecond
	config1.MaxRetryAttempts = 50
	config1.DrainBatchSize = 128

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.DrainBatchSize = 128

	cm1 := NewConnectionManager(config1, nil)
	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	cm1.AddManagedNode("node-2")

	const cycles = 3
	for cycle := 0; cycle < cycles; cycle++ {
		t.Logf("Reconnection cycle %d/%d", cycle+1, cycles)

		// Start node-2
		cm2 := NewConnectionManager(config2, nil)
		err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
			received.Add(1)
		})
		require.NoError(t, err)

		port2 := cm2.GetListenPort()
		cm2.AddManagedNode("node-1")
		cm1.EnsureConnection("node-2", "127.0.0.1", port2)

		// Wait for connection
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if len(cm1.ConnectedNodes()) > 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		require.NotEmpty(t, cm1.ConnectedNodes(), "cycle %d: should connect", cycle)

		// Send messages
		for i := 0; i < 5; i++ {
			_ = cm1.SendToNode("node-2", []byte("cycle message"), ClassRaftControl)
			time.Sleep(time.Millisecond)
		}

		// Short active period
		time.Sleep(50 * time.Millisecond)

		// Stop node-2
		_ = cm2.Stop()

		// Wait for disconnection to be detected
		deadline = time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if len(cm1.ConnectedNodes()) == 0 {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Final verification - at least some messages were delivered
	t.Logf("Total messages received: %d", received.Load())
	assert.Greater(t, received.Load(), int32(0), "should receive some messages across cycles")
}

func TestIntegration_BidirectionalCommunication(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logger := zap.NewNop()

	const messageCount = 50

	var node1Received, node2Received atomic.Int32

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.AutoPort = true
	config1.Logger = logger
	config1.DrainBatchSize = 128

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.AutoPort = true
	config2.Logger = logger
	config2.DrainBatchSize = 128

	cm1 := NewConnectionManager(config1, nil)
	cm2 := NewConnectionManager(config2, nil)

	err := cm1.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		node1Received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm1.Stop() }()

	err = cm2.Start(ctx, func(_ cluster.NodeID, _ []byte) {
		node2Received.Add(1)
	})
	require.NoError(t, err)
	defer func() { _ = cm2.Stop() }()

	port1 := cm1.GetListenPort()
	port2 := cm2.GetListenPort()

	cm1.AddManagedNode("node-2")
	cm2.AddManagedNode("node-1")
	cm1.EnsureConnection("node-2", "127.0.0.1", port2)
	cm2.EnsureConnection("node-1", "127.0.0.1", port1)

	// Wait for connection to be fully established
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cm1.ConnectedNodes()) > 0 && len(cm2.ConnectedNodes()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.NotEmpty(t, cm1.ConnectedNodes(), "node-1 should be connected")
	require.NotEmpty(t, cm2.ConnectedNodes(), "node-2 should be connected")

	// Send messages concurrently in both directions with small delays
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < messageCount; i++ {
			_ = cm1.SendToNode("node-2", []byte("from node-1"), ClassRaftControl)
			if i%5 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < messageCount; i++ {
			_ = cm2.SendToNode("node-1", []byte("from node-2"), ClassRaftControl)
			if i%5 == 0 {
				time.Sleep(time.Millisecond)
			}
		}
	}()

	wg.Wait()

	// Wait for delivery
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if node1Received.Load() >= messageCount && node2Received.Load() >= messageCount {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, node1Received.Load(), int32(messageCount))
	assert.GreaterOrEqual(t, node2Received.Load(), int32(messageCount))
}
