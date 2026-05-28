// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// TestMultiNodeClusterFormation tests that multiple nodes can form a cluster
func TestMultiNodeClusterFormation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := zap.NewNop()

	// Start bootstrap node
	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0, // Auto-assign port
		Meta:     cluster.NodeMeta{"role": "bootstrap"},
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	// Get the actual port node1 is listening on
	localNode1 := node1.LocalNode()
	joinAddr := localNode1.Addr

	// Track join events on node1
	var joinedNodes []string
	var joinMu sync.Mutex
	sub1, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeJoined, func(evt event.Event) {
		joinMu.Lock()
		joinedNodes = append(joinedNodes, evt.Path)
		joinMu.Unlock()
	})
	require.NoError(t, err)
	defer sub1.Close()

	// Start second node and join
	bus2 := eventbus.NewBus()
	node2 := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
		Meta:      cluster.NodeMeta{"role": "worker"},
	}, bus2, logger, nil, nil, nil)

	err = node2.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node2.Stop() }()

	// Start third node
	bus3 := eventbus.NewBus()
	node3 := NewService(Config{
		NodeName:  "node-3",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
		Meta:      cluster.NodeMeta{"role": "worker"},
	}, bus3, logger, nil, nil, nil)

	err = node3.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node3.Stop() }()

	// Wait for cluster to converge - all nodes should see 2 peers
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n1 := len(node1.Nodes())
		n2 := len(node2.Nodes())
		n3 := len(node3.Nodes())
		if n1 >= 2 && n2 >= 2 && n3 >= 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify all nodes see each other
	assert.GreaterOrEqual(t, len(node1.Nodes()), 2, "node-1 should see 2 other nodes")
	assert.GreaterOrEqual(t, len(node2.Nodes()), 2, "node-2 should see 2 other nodes")
	assert.GreaterOrEqual(t, len(node3.Nodes()), 2, "node-3 should see 2 other nodes")
}

// TestNodeLeaveDetection tests that nodes detect when a peer leaves
func TestNodeLeaveDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := zap.NewNop()

	// Start two nodes
	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	joinAddr := node1.LocalNode().Addr

	// Track leave events
	leftCh := make(chan string, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeLeft, func(evt event.Event) {
		select {
		case leftCh <- evt.Path:
		default:
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	bus2 := eventbus.NewBus()
	node2 := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
	}, bus2, logger, nil, nil, nil)

	err = node2.Start(ctx)
	require.NoError(t, err)

	// Wait for join
	time.Sleep(500 * time.Millisecond)
	require.Len(t, node1.Nodes(), 1, "node-1 should see node-2")

	// Gracefully stop node2
	_ = node2.Stop()

	// Wait for leave detection
	select {
	case leftNode := <-leftCh:
		assert.Equal(t, "node-2", leftNode)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for leave detection")
	}

	// Verify node1 no longer sees node2
	assert.Len(t, node1.Nodes(), 0, "node-1 should see no other nodes after node-2 left")
}

// TestNodeFailureDetection tests that crashed nodes are eventually detected
func TestNodeFailureDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := zap.NewNop()

	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	joinAddr := node1.LocalNode().Addr

	leftCh := make(chan string, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeLeft, func(evt event.Event) {
		select {
		case leftCh <- evt.Path:
		default:
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	bus2 := eventbus.NewBus()
	node2 := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
	}, bus2, logger, nil, nil, nil)

	err = node2.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)
	require.Len(t, node1.Nodes(), 1)

	// Simulate crash - shutdown memberlist without graceful leave
	_ = node2.memberlist.Shutdown()

	// Wait for failure detection (memberlist's suspicion mechanism)
	select {
	case leftNode := <-leftCh:
		assert.Equal(t, "node-2", leftNode)
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for failure detection")
	}
}

// TestConcurrentJoins tests multiple nodes joining simultaneously
func TestConcurrentJoins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := zap.NewNop()

	// Bootstrap node
	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "bootstrap",
		BindAddr: "127.0.0.1",
		BindPort: 0,
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	joinAddr := node1.LocalNode().Addr

	// Start 3 nodes concurrently (reduced for faster convergence)
	const numNodes = 3
	var nodes []*Service
	var nodesMu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numNodes; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			bus := eventbus.NewBus()
			node := NewService(Config{
				NodeName:  "node-" + string(rune('a'+idx)),
				BindAddr:  "127.0.0.1",
				BindPort:  0,
				JoinAddrs: []string{joinAddr},
			}, bus, logger, nil, nil, nil)

			if err := node.Start(ctx); err != nil {
				t.Errorf("node %d failed to start: %v", idx, err)
				return
			}

			nodesMu.Lock()
			nodes = append(nodes, node)
			nodesMu.Unlock()
		}(i)
	}

	wg.Wait()

	// Cleanup
	defer func() {
		for _, n := range nodes {
			_ = n.Stop()
		}
	}()

	// Wait for full gossip convergence
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		allConverged := true
		if len(node1.Nodes()) < numNodes {
			allConverged = false
		}
		for _, n := range nodes {
			if len(n.Nodes()) < numNodes {
				allConverged = false
				break
			}
		}
		if allConverged {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Bootstrap should see all joined nodes
	bootstrapPeers := len(node1.Nodes())
	assert.GreaterOrEqual(t, bootstrapPeers, numNodes, "bootstrap should see %d nodes, got %d", numNodes, bootstrapPeers)
}

// TestMetadataUpdate tests that metadata changes propagate
func TestMetadataUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := zap.NewNop()

	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
		Meta:     cluster.NodeMeta{"version": "1.0"},
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	joinAddr := node1.LocalNode().Addr

	updatedCh := make(chan cluster.NodeMeta, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeUpdated, func(evt event.Event) {
		nodeEvt := evt.Data.(cluster.NodeEvent)
		select {
		case updatedCh <- nodeEvt.Node.Meta:
		default:
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	bus2 := eventbus.NewBus()
	node2 := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
		Meta:      cluster.NodeMeta{"version": "1.0"},
	}, bus2, logger, nil, nil, nil)

	err = node2.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node2.Stop() }()

	time.Sleep(500 * time.Millisecond)

	// Update node2's metadata
	node2.config.Meta["version"] = "2.0"
	_ = node2.memberlist.UpdateNode(time.Second)

	// Wait for update event
	select {
	case meta := <-updatedCh:
		assert.Equal(t, "2.0", meta["version"])
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for metadata update")
	}
}

// TestClusterPartitionRecovery tests that nodes rejoin after network partition heals
func TestClusterPartitionRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger := zap.NewNop()

	// Create two nodes
	bus1 := eventbus.NewBus()
	node1 := NewService(Config{
		NodeName: "node-1",
		BindAddr: "127.0.0.1",
		BindPort: 0,
	}, bus1, logger, nil, nil, nil)

	err := node1.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node1.Stop() }()

	joinAddr := node1.LocalNode().Addr

	bus2 := eventbus.NewBus()
	node2 := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
	}, bus2, logger, nil, nil, nil)

	err = node2.Start(ctx)
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)
	require.Len(t, node1.Nodes(), 1)

	// Simulate partition by stopping node2
	_ = node2.Stop()

	// Wait for leave detection
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if len(node1.Nodes()) == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	require.Len(t, node1.Nodes(), 0, "node2 should be detected as gone")

	// Rejoin event tracking
	rejoinCh := make(chan struct{}, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeJoined, func(evt event.Event) {
		if evt.Path == "node-2" {
			select {
			case rejoinCh <- struct{}{}:
			default:
			}
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	// Restart node2 (simulating partition heal)
	bus2new := eventbus.NewBus()
	node2new := NewService(Config{
		NodeName:  "node-2",
		BindAddr:  "127.0.0.1",
		BindPort:  0,
		JoinAddrs: []string{joinAddr},
	}, bus2new, logger, nil, nil, nil)

	err = node2new.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = node2new.Stop() }()

	// Wait for rejoin. The deadline matches the leave-detection window
	// above (30s): a node restarting under the same name re-joins with a
	// fresh, low memberlist incarnation and must reconcile it against the
	// incarnation surviving peers recorded before it left. That
	// reconciliation takes several gossip cycles, and the -race detector's
	// ~10x slowdown pushes it past a tight 10s bound even though the
	// convergence itself is correct.
	select {
	case <-rejoinCh:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("timeout waiting for node2 to rejoin")
	}

	assert.Len(t, node1.Nodes(), 1, "node2 should have rejoined")
}
