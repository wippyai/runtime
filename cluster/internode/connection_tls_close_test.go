// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"crypto/tls"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func idleTLSPair(t *testing.T) (*tls.Conn, *tls.Conn) {
	t.Helper()
	one, two := managerTestTLSConfigs(t)
	clientConfig, err := loadTLSConfig(one)
	require.NoError(t, err)
	serverConfig, err := loadTLSConfig(two)
	require.NoError(t, err)
	clientConfig.ServerName = "127.0.0.1"
	serverRaw, clientRaw := net.Pipe()
	t.Cleanup(func() { _ = serverRaw.Close(); _ = clientRaw.Close() })
	client := tls.Client(clientRaw, clientConfig)
	server := tls.Server(serverRaw, serverConfig)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverHandshake := make(chan error, 1)
	go func() { serverHandshake <- server.HandshakeContext(ctx) }()
	require.NoError(t, client.HandshakeContext(ctx))
	require.NoError(t, <-serverHandshake)
	require.True(t, client.ConnectionState().HandshakeComplete)
	require.NotEmpty(t, server.ConnectionState().VerifiedChains, "server must authenticate the client")

	return client, server
}

func TestNodeConnectionTLSCloseWithoutPeerRead(t *testing.T) {
	client, _ := idleTLSPair(t)

	// The authenticated peer deliberately performs no more reads. Closing the
	// runtime session must release its transport without waiting for close_notify.
	node := newNodeConnection(client, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
	done := make(chan struct{})
	go func() { node.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		// Release and join the goroutine even when checking the unfixed version.
		_ = client.NetConn().Close()
		<-done
		t.Fatal("runtime TLS shutdown waited for an idle peer to read close_notify")
	}
	node.Close() // repeated shutdown is safe
}

func TestClientHandshakeRejectionDoesNotWaitForTLSCloseNotify(t *testing.T) {
	client, server := idleTLSPair(t)
	peerDone := make(chan error, 1)
	go func() {
		_, err := readPrefixedBytes(server, maxNodeIDLength)
		if err == nil {
			err = writePrefixedBytes(server, []byte("unexpected-peer"))
		}
		peerDone <- err
		// No further read: the peer has supplied an invalid identity.
	}()
	done := make(chan error, 1)
	go func() {
		_, err := PerformClientHandshake(client, DefaultNodeConnectionConfig(), zap.NewNop(), "self", "expected-peer")
		done <- err
	}()
	require.NoError(t, <-peerDone)
	select {
	case err := <-done:
		require.ErrorContains(t, err, "node ID mismatch")
	case <-time.After(time.Second):
		_ = client.NetConn().Close()
		<-done
		t.Fatal("handshake rejection waited for peer to read close_notify")
	}
}
