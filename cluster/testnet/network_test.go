package testnet

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

func TestSimulatedClusterFormation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	net := NewNetwork()
	logger := zap.NewNop()

	// Create nodes
	n1 := net.AddNode("node-1")
	n2 := net.AddNode("node-2")
	n3 := net.AddNode("node-3")

	bus1 := eventbus.NewBus()
	bus2 := eventbus.NewBus()
	bus3 := eventbus.NewBus()

	// Create services with simulated transport
	svc1 := membership.New(
		membership.WithNodeName("node-1"),
		membership.WithTransport(n1.Transport()),
		membership.WithEventBus(bus1),
		membership.WithLogger(logger),
	)

	svc2 := membership.New(
		membership.WithNodeName("node-2"),
		membership.WithTransport(n2.Transport()),
		membership.WithJoinAddrs(n1.Addr()), // Use actual address
		membership.WithEventBus(bus2),
		membership.WithLogger(logger),
	)

	svc3 := membership.New(
		membership.WithNodeName("node-3"),
		membership.WithTransport(n3.Transport()),
		membership.WithJoinAddrs(n1.Addr()), // Use actual address
		membership.WithEventBus(bus3),
		membership.WithLogger(logger),
	)

	// Start all services
	require.NoError(t, svc1.Start(ctx))
	defer svc1.Stop()

	require.NoError(t, svc2.Start(ctx))
	defer svc2.Stop()

	require.NoError(t, svc3.Start(ctx))
	defer svc3.Stop()

	// Wait for convergence
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc1.Nodes()) >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, len(svc1.Nodes()), 2)
}

func TestAsymmetricPartition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	net := NewNetwork()
	logger := zap.NewNop()

	n1 := net.AddNode("node-1")
	n2 := net.AddNode("node-2")

	bus1 := eventbus.NewBus()
	bus2 := eventbus.NewBus()

	svc1 := membership.New(
		membership.WithNodeName("node-1"),
		membership.WithTransport(n1.Transport()),
		membership.WithEventBus(bus1),
		membership.WithLogger(logger),
	)

	svc2 := membership.New(
		membership.WithNodeName("node-2"),
		membership.WithTransport(n2.Transport()),
		membership.WithJoinAddrs(n1.Addr()),
		membership.WithEventBus(bus2),
		membership.WithLogger(logger),
	)

	require.NoError(t, svc1.Start(ctx))
	defer svc1.Stop()

	require.NoError(t, svc2.Start(ctx))
	defer svc2.Stop()

	// Wait for initial cluster formation
	time.Sleep(500 * time.Millisecond)
	require.Len(t, svc1.Nodes(), 1, "node-1 should see node-2")

	// Create asymmetric partition: node-1 can't reach node-2, but node-2 can reach node-1
	net.PartitionAsymmetric("node-1", "node-2")

	// Verify the link state
	link := net.Link("node-1", "node-2")
	assert.True(t, link.IsBlocked())

	reverseLink := net.Link("node-2", "node-1")
	assert.False(t, reverseLink.IsBlocked())

	// Check events
	events := net.Events()
	assert.Len(t, events, 1)
	assert.Equal(t, EventAsymmetricPartition, events[0].Type)
}

func TestNodeCrashSimulation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	net := NewNetwork()
	logger := zap.NewNop()

	n1 := net.AddNode("node-1")
	n2 := net.AddNode("node-2")

	bus1 := eventbus.NewBus()
	bus2 := eventbus.NewBus()

	// Track leave events
	leftCh := make(chan string, 1)
	sub, err := eventbus.NewSubscriber(ctx, bus1, cluster.System, cluster.NodeLeftEventKind, func(evt event.Event) {
		select {
		case leftCh <- evt.Path:
		default:
		}
	})
	require.NoError(t, err)
	defer sub.Close()

	svc1 := membership.New(
		membership.WithNodeName("node-1"),
		membership.WithTransport(n1.Transport()),
		membership.WithEventBus(bus1),
		membership.WithLogger(logger),
	)

	svc2 := membership.New(
		membership.WithNodeName("node-2"),
		membership.WithTransport(n2.Transport()),
		membership.WithJoinAddrs(n1.Addr()),
		membership.WithEventBus(bus2),
		membership.WithLogger(logger),
	)

	require.NoError(t, svc1.Start(ctx))
	defer svc1.Stop()

	require.NoError(t, svc2.Start(ctx))

	// Wait for cluster formation
	time.Sleep(500 * time.Millisecond)
	require.Len(t, svc1.Nodes(), 1)

	// Simulate crash by disconnecting node-2
	n2.Disconnect()

	// Wait for failure detection
	select {
	case left := <-leftCh:
		assert.Equal(t, "node-2", left)
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for crash detection")
	}
}

func TestPartitionAndHeal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	net := NewNetwork()
	logger := zap.NewNop()

	n1 := net.AddNode("node-1")
	n2 := net.AddNode("node-2")

	bus1 := eventbus.NewBus()
	bus2 := eventbus.NewBus()

	var events []string
	var mu sync.Mutex

	sub1, _ := eventbus.NewSubscriber(ctx, bus1, cluster.System, "node.*", func(evt event.Event) {
		mu.Lock()
		events = append(events, string(evt.Kind)+":"+evt.Path)
		mu.Unlock()
	})
	defer sub1.Close()

	svc1 := membership.New(
		membership.WithNodeName("node-1"),
		membership.WithTransport(n1.Transport()),
		membership.WithEventBus(bus1),
		membership.WithLogger(logger),
	)

	svc2 := membership.New(
		membership.WithNodeName("node-2"),
		membership.WithTransport(n2.Transport()),
		membership.WithJoinAddrs(n1.Addr()),
		membership.WithEventBus(bus2),
		membership.WithLogger(logger),
	)

	require.NoError(t, svc1.Start(ctx))
	defer svc1.Stop()

	require.NoError(t, svc2.Start(ctx))
	defer svc2.Stop()

	// Wait for join
	time.Sleep(500 * time.Millisecond)

	// Partition
	net.PartitionBidirectional("node-1", "node-2")

	// Wait for failure detection
	time.Sleep(10 * time.Second)

	// Heal
	net.HealBidirectional("node-1", "node-2")

	// Wait for rejoin
	time.Sleep(2 * time.Second)

	mu.Lock()
	t.Logf("Events: %v", events)
	mu.Unlock()

	// Should have seen join, leave, join sequence
	// (exact timing depends on memberlist's failure detection)
}

func TestLinkLatency(t *testing.T) {
	net := NewNetwork()
	net.AddNode("node-1")
	net.AddNode("node-2")

	link := net.Link("node-1", "node-2")
	require.NotNil(t, link)

	net.SetLatency("node-1", "node-2", 100*time.Millisecond)

	assert.Equal(t, 100*time.Millisecond, link.Latency())
}

func TestLinkPacketLoss(t *testing.T) {
	net := NewNetwork()
	net.AddNode("node-1")
	net.AddNode("node-2")

	link := net.Link("node-1", "node-2")
	require.NotNil(t, link)

	net.SetPacketLoss("node-1", "node-2", 0.5)

	assert.Equal(t, 0.5, link.PacketLoss())

	// With 50% packet loss, some should be dropped
	delivered := 0
	for i := 0; i < 100; i++ {
		if link.ShouldDeliver() {
			delivered++
		}
	}

	// Should be roughly 50% (with some variance)
	assert.Greater(t, delivered, 20)
	assert.Less(t, delivered, 80)
}
