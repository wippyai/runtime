// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"go.uber.org/zap"
)

func TestConnectedPeerRetainsUpdatedEndpointForRetry(t *testing.T) {
	cfg := insecureManagerConfig()
	cfg.Logger = zap.NewNop()
	m := NewConnectionManager(cfg, nil).(*manager)
	m.AddManagedNode("peer")
	defer m.RemoveManagedNode("peer")
	m.nodeStates.UpdateNodeAddress("peer", "127.0.0.1", 9100)
	m.nodeStates.SetNodeConnection("peer", nil, StateConnected)
	m.EnsureConnection("peer", "127.0.0.2", 9200)
	addr, port, ok := m.nodeStates.GetNodeAddress("peer")
	require.True(t, ok)
	require.Equal(t, "127.0.0.2", addr)
	require.Equal(t, 9200, port)
	_, state := m.nodeStates.GetNodeConnection("peer")
	require.Equal(t, StateConnected, state)
}

func TestServiceMembershipUpdateRefreshesManagedEndpoint(t *testing.T) {
	service, manager, _, bus, ctx, cancel := setupService(t)
	defer cancel()
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	manager.AddManagedNode("peer")
	bus.Send(ctx, event.Event{
		System: cluster.System, Kind: cluster.NodeUpdated, Path: "node.updated",
		Data: cluster.NodeEvent{Node: cluster.NodeInfo{
			ID: "peer", Addr: "127.0.0.1:7946",
			Meta: cluster.NodeMeta{MetadataPort: "9100"},
		}},
	})
	require.Eventually(t, func() bool {
		manager.mu.Lock()
		defer manager.mu.Unlock()
		for _, call := range manager.ensuredConns {
			if call == (ensureConnCall{nodeID: "peer", addr: "127.0.0.1", port: 9100}) {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
}

func TestServiceMembershipUpdateDoesNotReadmitDepartedPeer(t *testing.T) {
	service, manager, _, _, ctx, cancel := setupService(t)
	defer cancel()
	require.NoError(t, service.Start(ctx))
	defer service.Stop()
	manager.AddManagedNode("departed")
	manager.RemoveManagedNode("departed")
	service.handleMembershipEvent(event.Event{
		Kind: cluster.NodeUpdated,
		Data: cluster.NodeEvent{Node: cluster.NodeInfo{
			ID: "departed", Addr: "127.0.0.1:7946",
			Meta: cluster.NodeMeta{MetadataPort: "9100"},
		}},
	})
	manager.mu.Lock()
	defer manager.mu.Unlock()
	require.False(t, manager.managedNodes["departed"])
	for _, call := range manager.ensuredConns {
		require.NotEqual(t, "departed", call.nodeID)
	}
}
