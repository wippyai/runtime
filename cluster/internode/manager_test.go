package internode

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateNone, "NONE"},
		{StateConnecting, "CONNECTING"},
		{StateConnected, "CONNECTED"},
		{StateRetrying, "RETRYING"},
		{StateDead, "DEAD"},
		{ConnectionState(999), "UNKNOWN"},
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

	manager := NewConnectionManager(config)

	assert.NotNil(t, manager)
}

func TestManager_GetListenPort(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "test-node"
	config.BindAddr = "127.0.0.1"
	config.BindPort = 9500
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config).(*manager)

	manager.actualPort = 9500

	port := manager.GetListenPort()
	assert.Equal(t, 9500, port)
}

func TestManager_AddManagedNode(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config).(*manager)

	nodeID := cluster.NodeID("remote-node")
	manager.AddManagedNode(nodeID)
}

func TestManager_RemoveManagedNode(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config).(*manager)

	nodeID := cluster.NodeID("remote-node")
	manager.RemoveManagedNode(nodeID)
}

func TestManager_ConnectedNodes(t *testing.T) {
	config := DefaultManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config).(*manager)

	nodes := manager.ConnectedNodes()
	assert.Len(t, nodes, 0)
}
