// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"go.uber.org/zap"
)

func TestHandshake_Success(t *testing.T) {
	nodeAID, nodeBID := "node-A", "node-B"
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

func TestHandshake_Authenticated(t *testing.T) {
	clientPublicKey, clientSigningKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	serverPublicKey, serverSigningKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, attackerSigningKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	tests := []struct {
		name             string
		clientKey        string
		serverKey        string
		clientSigningKey ed25519.PrivateKey
		authorizePeer    bool
		wantError        bool
	}{
		{name: "matching identity", clientKey: "shared-secret", serverKey: "shared-secret", clientSigningKey: clientSigningKey, authorizePeer: true},
		{name: "wrong client key", clientKey: "attacker-secret", serverKey: "shared-secret", clientSigningKey: clientSigningKey, authorizePeer: true, wantError: true},
		{name: "forged client identity", clientKey: "shared-secret", serverKey: "shared-secret", clientSigningKey: attackerSigningKey, authorizePeer: true, wantError: true},
		{name: "unauthorized peer", clientKey: "shared-secret", serverKey: "shared-secret", clientSigningKey: clientSigningKey, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientCfg := DefaultNodeConnectionConfig()
			clientCfg.RequireAuthentication = true
			clientCfg.AuthenticationKey = []byte(tt.clientKey)
			clientCfg.SigningKey = tt.clientSigningKey
			clientCfg.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) {
				return serverPublicKey, id == "node-B"
			}
			serverCfg := DefaultNodeConnectionConfig()
			serverCfg.RequireAuthentication = true
			serverCfg.AuthenticationKey = []byte(tt.serverKey)
			serverCfg.SigningKey = serverSigningKey
			serverCfg.ResolvePeerKey = func(id cluster.NodeID) (ed25519.PublicKey, bool) {
				return clientPublicKey, id == "node-A"
			}
			serverCfg.AuthorizePeer = func(id cluster.NodeID, _ net.Addr) bool {
				return tt.authorizePeer && id == "node-A"
			}
			serverConn, clientConn := net.Pipe()
			clientErrors := make(chan error, 1)
			serverErrors := make(chan error, 1)

			go func() {
				nodeConn, err := PerformClientHandshake(clientConn, clientCfg, zap.NewNop(), "node-A", "node-B")
				if nodeConn != nil {
					_ = nodeConn.conn.Close()
				}
				clientErrors <- err
			}()
			go func() {
				nodeConn, err := PerformServerHandshake(serverConn, serverCfg, zap.NewNop(), "node-B")
				if nodeConn != nil {
					_ = nodeConn.conn.Close()
				}
				serverErrors <- err
			}()

			clientErr := <-clientErrors
			serverErr := <-serverErrors
			if tt.wantError {
				require.Error(t, errors.Join(clientErr, serverErr))
				return
			}
			require.NoError(t, clientErr)
			require.NoError(t, serverErr)
		})
	}
}

func TestHandshake_Client_UnexpectedRemoteID(t *testing.T) {
	nodeAID, nodeBID := "node-A", "node-B"
	wrongNodeID := "node-C"
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
	clientConnErr := &ConnectionError{}
	ok := errors.As(clientErr, &clientConnErr)
	require.True(t, ok)
	require.Equal(t, ExitProtocolError, clientConnErr.Reason)
	require.Contains(t, clientConnErr.Error(), "node ID mismatch")

	// The server should succeed - it completed its handshake correctly
	// Server doesn't know what the client expected
	require.NoError(t, serverErr)
}
