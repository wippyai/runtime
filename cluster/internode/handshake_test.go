package internode

import (
	"github.com/ponyruntime/pony/api/cluster"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net"
	"sync"
	"testing"
)

func TestHandshake_Success(t *testing.T) {
	nodeAID, nodeBID := cluster.NodeID("node-A"), cluster.NodeID("node-B")
	cfg := DefaultNodeConnectionConfig()
	logger := zap.NewNop()

	serverConn, clientConn := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(2)

	var clientErr, serverErr error
	var clientNodeConn, serverNodeConn *NodeConnection

	go func() {
		defer wg.Done()
		clientNodeConn, clientErr = PerformClientHandshake(clientConn, cfg, logger, nodeAID, nodeBID)
	}()

	go func() {
		defer wg.Done()
		serverNodeConn, serverErr = PerformServerHandshake(serverConn, cfg, logger, nodeBID)
	}()

	wg.Wait()

	require.NoError(t, clientErr)
	require.NoError(t, serverErr)
	require.NotNil(t, clientNodeConn)
	require.NotNil(t, serverNodeConn)

	// Test now owns the connections and is responsible for closing them.
	defer require.NoError(t, clientNodeConn.conn.Close())
	defer require.NoError(t, serverNodeConn.conn.Close())

	require.Equal(t, nodeBID, clientNodeConn.RemoteNodeID())
	require.Equal(t, nodeAID, serverNodeConn.RemoteNodeID())
}

func TestHandshake_Client_UnexpectedRemoteID(t *testing.T) {
	nodeAID, nodeBID := cluster.NodeID("node-A"), cluster.NodeID("node-B")
	wrongNodeID := cluster.NodeID("node-C")
	cfg := DefaultNodeConnectionConfig()
	logger := zap.NewNop()

	serverConn, clientConn := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(2)

	clientErrChan := make(chan error, 1)
	serverErrChan := make(chan error, 1)

	go func() {
		defer wg.Done()
		_, err := PerformClientHandshake(clientConn, cfg, logger, nodeAID, nodeBID)
		clientErrChan <- err
	}()

	go func() {
		defer wg.Done()
		_, err := PerformServerHandshake(serverConn, cfg, logger, wrongNodeID)
		serverErrChan <- err
	}()

	wg.Wait()

	clientErr := <-clientErrChan
	serverErr := <-serverErrChan

	// The client MUST fail with a protocol error (wrong remote node ID)
	require.Error(t, clientErr)
	clientConnErr, ok := clientErr.(*ConnectionError)
	require.True(t, ok)
	require.Equal(t, ExitProtocolError, clientConnErr.Reason)
	require.Contains(t, clientConnErr.Error(), "expected remote node ID 'node-B' but got 'node-C'")

	// The server should succeed - it completed its handshake correctly
	// Server doesn't know what the client expected
	require.NoError(t, serverErr)
}
