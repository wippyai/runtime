// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		expected string
		state    ConnectionState
	}{
		{"NONE", StateNone},
		{"CONNECTING", StateConnecting},
		{"CONNECTED", StateConnected},
		{"RETRYING", StateRetrying},
		{"DEAD", StateDead},
		{"UNKNOWN", ConnectionState(999)},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestDefaultManagerConfig(t *testing.T) {
	config := DefaultManagerConfig()

	assert.Equal(t, 5*time.Second, config.HandshakeTimeout)
	assert.Equal(t, 256, config.OutboundQueueSize)
	assert.Equal(t, uint32(512*1024*1024), config.MaxMessageSize)
	assert.False(t, config.TLS.Enabled)
	assert.Equal(t, 10*time.Millisecond, config.InitialRetryDelay)
	assert.Equal(t, 5*time.Second, config.MaxRetryDelay)
	assert.True(t, config.AutoPort)
	assert.Equal(t, DefaultPortRangeStart, config.BindPort)
	assert.Equal(t, 32, config.DrainBatchSize)
	assert.Equal(t, 256, config.CommandQueueSize)
	assert.Equal(t, 10, config.MaxRetryAttempts)
	assert.Equal(t, 4096, config.RaftControlQueueCap)
	assert.Equal(t, 1024, config.GossipQueueCap)
	assert.Equal(t, 2048, config.PGBroadcastQueueCap)
}

func TestManagerConfig_NodeConnectionConfig(t *testing.T) {
	config := ManagerConfig{
		HandshakeTimeout: 3 * time.Second,
		MaxMessageSize:   1024 * 1024,
	}

	nodeConfig := config.NodeConnectionConfig()

	assert.Equal(t, 3*time.Second, nodeConfig.HandshakeTimeout)
	assert.Equal(t, uint32(1024*1024), nodeConfig.MaxMessageSize)
}

func TestNewConnectionManager(t *testing.T) {
	config := ManagerConfig{
		LocalNodeID:       "test-node",
		BindAddr:          "127.0.0.1",
		BindPort:          9000,
		HandshakeTimeout:  5 * time.Second,
		OutboundQueueSize: 128,
		MaxMessageSize:    1024,
		Logger:            zap.NewNop(),
		DrainBatchSize:    16,
		CommandQueueSize:  64,
	}

	manager := NewConnectionManager(config, nil)

	assert.NotNil(t, manager)
}

func TestManager_GetListenPort(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "test-node"
	config.BindAddr = "127.0.0.1"
	config.BindPort = 9500
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	manager.actualPort = 9500

	port := manager.GetListenPort()
	assert.Equal(t, 9500, port)
}

func TestManager_AddManagedNode(_ *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)
}

func TestManager_RemoveManagedNode(_ *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.RemoveManagedNode(nodeID)
}

func TestManager_ConnectedNodes(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodes := manager.ConnectedNodes()
	assert.Len(t, nodes, 0)
}

// TestManager_RaftMeshOverflow_InvokesResetHandler proves the end-to-end
// reset wiring: when a ClassRaftMesh queue overflows, SendToNode invokes
// the registered overflow handler for that peer (the seam the raft mesh
// layer uses to tear down + rebuild the yamux session) and returns nil to
// the producer so a reset never surfaces as a blocking write error.
func TestManager_RaftMeshOverflow_InvokesResetHandler(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()
	config.RaftMeshQueueCap = 2
	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)

	var reset atomic.Int32
	resetPeer := make(chan cluster.NodeID, 1)
	require.True(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, func(peer cluster.NodeID) {
		reset.Add(1)
		select {
		case resetPeer <- peer:
		default:
		}
	}))

	// Fill to capacity, then overflow.
	require.NoError(t, manager.SendToNode(nodeID, []byte{0}, ClassRaftMesh))
	require.NoError(t, manager.SendToNode(nodeID, []byte{1}, ClassRaftMesh))
	// The overflowing send must not surface an error to the producer; the
	// reset is dispatched asynchronously.
	require.NoError(t, manager.SendToNode(nodeID, []byte{2}, ClassRaftMesh))

	select {
	case peer := <-resetPeer:
		assert.Equal(t, nodeID, peer)
	case <-time.After(time.Second):
		t.Fatal("overflow handler was not invoked on ClassRaftMesh overflow")
	}
	assert.Equal(t, int32(1), reset.Load())
}

// TestManager_RegisterClassOverflowHandler_SingleRegistration asserts the
// handler slot is single-claim (mirrors RegisterClassReceiver) and that
// passing nil clears it.
func TestManager_RegisterClassOverflowHandler_SingleRegistration(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()
	manager := NewConnectionManager(config, nil).(*manager)

	require.True(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, func(cluster.NodeID) {}))
	require.False(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, func(cluster.NodeID) {}),
		"a second non-nil registration must be rejected")
	require.True(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, nil),
		"nil clears the handler")
	require.True(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, func(cluster.NodeID) {}),
		"re-register after clear succeeds")
}

// TestManager_NonMeshOverflow_NoResetHandler guards that the reset path is
// ClassRaftMesh-only: a drop-newest class hitting its cap returns
// ErrQueueFull and never fires the overflow handler.
func TestManager_NonMeshOverflow_NoResetHandler(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()
	config.GossipQueueCap = 1
	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)

	var reset atomic.Int32
	require.True(t, manager.RegisterClassOverflowHandler(ClassRaftMesh, func(cluster.NodeID) {
		reset.Add(1)
	}))

	require.NoError(t, manager.SendToNode(nodeID, []byte{0}, ClassGossip))
	// Gossip overflow: SendToNode propagates ErrQueueFull, no reset.
	require.ErrorIs(t, manager.SendToNode(nodeID, []byte{1}, ClassGossip), ErrQueueFull)

	// Give any (erroneous) async handler a chance to fire.
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), reset.Load())
}
