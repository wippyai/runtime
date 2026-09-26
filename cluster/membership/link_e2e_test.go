// SPDX-License-Identifier: MPL-2.0

package membership

import (
	"context"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/memberlist"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type linkedNode struct {
	membership *Service
	links      internode.ConnectionManager
	received   *atomic.Int64
	left       *atomic.Int32
	name       string
}

func startLinkedNode(ctx context.Context, t *testing.T, name string, transport memberlist.Transport, join []string) linkedNode {
	t.Helper()
	cfg := internode.DefaultManagerConfig()
	cfg.LocalNodeID = name
	cfg.BindAddr = "127.0.0.1"
	cfg.BindPort = 0
	cfg.RequireAuthentication = false
	cfg.Logger = zap.NewNop()
	cfg.InitialRetryDelay = 5 * time.Millisecond
	cfg.MaxRetryDelay = 50 * time.Millisecond
	links := internode.NewConnectionManager(cfg, nil)
	received := &atomic.Int64{}
	require.NoError(t, links.Start(ctx, func(_ cluster.NodeID, data []byte) { received.Add(int64(len(data))) }, func(cluster.NodeID) {}))
	t.Cleanup(func() { _ = links.Stop() })

	bus := eventbus.NewBus()
	left := &atomic.Int32{}
	sub, err := eventbus.NewSubscriber(ctx, bus, cluster.System, cluster.NodeLeft, func(event.Event) { left.Add(1) })
	require.NoError(t, err)
	t.Cleanup(sub.Close)

	// Production probe timing: the load must not starve probes at the
	// intervals a real cluster uses.
	service := NewService(Config{NodeName: name, Transport: transport, Link: links, JoinAddrs: join}, bus, zap.NewNop(), nil, nil, nil)
	require.NoError(t, service.Start(ctx))
	t.Cleanup(func() { _ = service.Stop() })
	return linkedNode{membership: service, links: links, received: received, left: left, name: name}
}

func connectLinks(nodes []linkedNode) {
	for _, from := range nodes {
		for _, to := range nodes {
			if from.name == to.name {
				continue
			}
			from.links.AddManagedNode(to.name)
			from.links.EnsureConnection(to.name, "127.0.0.1", to.links.GetListenPort())
		}
	}
}

// Three nodes, one of them reachable only through its own connections, keep a
// stable membership while their links carry sustained megabyte frames: probes
// reach the one-way node over its links, and reach the others over the
// memberlist transport without waiting behind the bulk traffic.
func TestLinkedClusterStaysStableUnderLoadWithOneWayNode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping multi-node load test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	network := &memberlist.MockNetwork{}
	mockA := network.NewTransport("node-a")
	mockB := network.NewTransport("node-b")
	mockC := network.NewTransport("node-c")
	hostC, portC, err := mockC.FinalAdvertiseAddr("", 0)
	require.NoError(t, err)
	unreachable := memberlist.Address{Addr: net.JoinHostPort(hostC.String(), strconv.Itoa(portC)), Name: "node-c"}
	hostA, portA, err := mockA.FinalAdvertiseAddr("", 0)
	require.NoError(t, err)
	join := []string{net.JoinHostPort(hostA.String(), strconv.Itoa(portA))}

	a := startLinkedNode(ctx, t, "node-a", &oneWayTransport{MockTransport: mockA, unreachable: unreachable}, nil)
	b := startLinkedNode(ctx, t, "node-b", &oneWayTransport{MockTransport: mockB, unreachable: unreachable}, join)
	c := startLinkedNode(ctx, t, "node-c", mockC, join)
	nodes := []linkedNode{a, b, c}
	connectLinks(nodes)

	require.Eventually(t, func() bool {
		for _, n := range nodes {
			if len(n.membership.Nodes()) != 2 {
				return false
			}
			for _, peer := range nodes {
				if peer.name == n.name {
					continue
				}
				if _, ok := n.links.Link(peer.name); !ok {
					return false
				}
			}
		}
		return true
	}, 10*time.Second, 10*time.Millisecond)
	for _, n := range nodes {
		n.left.Store(0)
	}

	frame := make([]byte, 1<<20)
	loadCtx, stopLoad := context.WithCancel(ctx)
	defer stopLoad()
	for _, from := range nodes {
		for _, to := range nodes {
			if from.name == to.name {
				continue
			}
			go func(from linkedNode, to string) {
				for loadCtx.Err() == nil {
					if err := from.links.SendToNode(to, frame, internode.ClassPGBroadcast); err != nil {
						time.Sleep(time.Millisecond)
					}
				}
			}(from, to.name)
		}
	}

	require.Never(t, func() bool {
		for _, n := range nodes {
			if n.left.Load() != 0 || len(n.membership.Nodes()) != 2 {
				t.Logf("%s: left=%d members=%d", n.name, n.left.Load(), len(n.membership.Nodes()))
				return true
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond)
	stopLoad()

	for _, n := range nodes {
		require.Greater(t, n.received.Load(), int64(64<<20), "%s received too little bulk traffic to load its links", n.name)
	}
}
