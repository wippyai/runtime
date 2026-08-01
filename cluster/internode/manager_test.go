// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func insecureManagerConfig() ManagerConfig {
	config := DefaultManagerConfig()
	config.RequireAuthentication = false
	return config
}

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
	assert.Equal(t, 1024, config.GossipQueueCap)
	assert.True(t, config.RequireAuthentication)
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

func TestManager_RequiresAuthenticationConfiguration(t *testing.T) {
	publicKey, signingKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	baseConfig := func() ManagerConfig {
		config := insecureManagerConfig()
		config.LocalNodeID = "test-node"
		config.BindAddr = "127.0.0.1"
		config.Logger = zap.NewNop()
		config.RequireAuthentication = true
		config.AuthenticationKey = []byte("shared-secret")
		config.SigningKey = signingKey
		config.ResolvePeerKey = func(cluster.NodeID) (ed25519.PublicKey, bool) { return publicKey, true }
		config.AuthorizePeer = func(cluster.NodeID, net.Addr) bool { return true }
		return config
	}

	t.Run("missing key", func(t *testing.T) {
		config := baseConfig()
		config.AuthenticationKey = nil
		manager := NewConnectionManager(config, nil)
		require.Error(t, manager.Start(context.Background(), func(cluster.NodeID, []byte) {}))
		require.NoError(t, manager.Stop())
	})

	t.Run("missing signing key", func(t *testing.T) {
		config := baseConfig()
		config.SigningKey = nil
		manager := NewConnectionManager(config, nil)
		require.Error(t, manager.Start(context.Background(), func(cluster.NodeID, []byte) {}))
	})

	t.Run("missing peer key resolver", func(t *testing.T) {
		config := baseConfig()
		config.ResolvePeerKey = nil
		manager := NewConnectionManager(config, nil)
		require.Error(t, manager.Start(context.Background(), func(cluster.NodeID, []byte) {}))
	})

	t.Run("missing authorizer", func(t *testing.T) {
		config := baseConfig()
		config.AuthorizePeer = nil
		manager := NewConnectionManager(config, nil)
		require.Error(t, manager.Start(context.Background(), func(cluster.NodeID, []byte) {}))
	})
}

func TestManager_AuthenticatedCommunication(t *testing.T) {
	publicKey1, privateKey1, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	publicKey2, privateKey2, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sharedKey := []byte("0123456789abcdef0123456789abcdef")

	config1 := DefaultManagerConfig()
	config1.LocalNodeID = "node-1"
	config1.BindAddr = "127.0.0.1"
	config1.Logger = zap.NewNop()
	config1.AuthenticationKey = sharedKey
	config1.SigningKey = privateKey1
	config1.ResolvePeerKey = func(nodeID cluster.NodeID) (ed25519.PublicKey, bool) {
		return publicKey2, nodeID == "node-2"
	}
	config1.AuthorizePeer = func(nodeID cluster.NodeID, _ net.Addr) bool { return nodeID == "node-2" }

	config2 := DefaultManagerConfig()
	config2.LocalNodeID = "node-2"
	config2.BindAddr = "127.0.0.1"
	config2.Logger = zap.NewNop()
	config2.AuthenticationKey = sharedKey
	config2.SigningKey = privateKey2
	config2.ResolvePeerKey = func(nodeID cluster.NodeID) (ed25519.PublicKey, bool) {
		return publicKey1, nodeID == "node-1"
	}
	config2.AuthorizePeer = func(nodeID cluster.NodeID, _ net.Addr) bool { return nodeID == "node-1" }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	received := make(chan []byte, 1)
	manager1 := NewConnectionManager(config1, nil)
	manager2 := NewConnectionManager(config2, nil)
	require.NoError(t, manager1.Start(ctx, func(cluster.NodeID, []byte) {}))
	defer func() { require.NoError(t, manager1.Stop()) }()
	require.NoError(t, manager2.Start(ctx, func(nodeID cluster.NodeID, data []byte) {
		if nodeID == "node-1" {
			received <- append([]byte(nil), data...)
		}
	}))
	defer func() { require.NoError(t, manager2.Stop()) }()

	manager1.AddManagedNode("node-2")
	manager2.AddManagedNode("node-1")
	manager1.EnsureConnection("node-2", "127.0.0.1", manager2.GetListenPort())
	require.Eventually(t, func() bool {
		return len(manager1.ConnectedNodes()) == 1 && len(manager2.ConnectedNodes()) == 1
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, manager1.SendToNode("node-2", []byte("authenticated"), ClassRaftControl))

	select {
	case message := <-received:
		require.Equal(t, []byte("authenticated"), message)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
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
	config := insecureManagerConfig()
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
	config := insecureManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)
}

func TestManager_RemoveManagedNode(_ *testing.T) {
	config := insecureManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.RemoveManagedNode(nodeID)
}

func TestManager_ConnectedNodes(t *testing.T) {
	config := insecureManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()

	manager := NewConnectionManager(config, nil).(*manager)

	nodes := manager.ConnectedNodes()
	assert.Len(t, nodes, 0)
}

func TestManager_RaftRPCDoesNotOverflow(t *testing.T) {
	config := insecureManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()
	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)

	for i := 0; i < 100; i++ {
		require.NoError(t, manager.SendToNode(nodeID, []byte{byte(i)}, ClassRaftRPC))
	}
}

func TestManager_GossipOverflow_ReturnsQueueFull(t *testing.T) {
	config := insecureManagerConfig()
	config.LocalNodeID = "local-node"
	config.Logger = zap.NewNop()
	config.GossipQueueCap = 1
	manager := NewConnectionManager(config, nil).(*manager)

	nodeID := "remote-node"
	manager.AddManagedNode(nodeID)

	require.NoError(t, manager.SendToNode(nodeID, []byte{0}, ClassGossip))
	require.ErrorIs(t, manager.SendToNode(nodeID, []byte{1}, ClassGossip), ErrQueueFull)
}
