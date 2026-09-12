// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type observedHandshakeRead struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (c *observedHandshakeRead) Read(data []byte) (int, error) {
	c.once.Do(func() { close(c.entered) })
	return c.Conn.Read(data)
}

func TestManagerStopAbortsStalledTLSHandshake(t *testing.T) {
	tlsFiles, _ := managerTestTLSConfigs(t)
	serverTLS, err := loadTLSConfig(tlsFiles)
	require.NoError(t, err)
	cfg := DefaultManagerConfig()
	cfg.LocalNodeID, cfg.Logger = "receiver", zap.NewNop()
	cfg.HandshakeTimeout = time.Hour // cancellation must not depend on this deadline
	manager := NewConnectionManager(cfg, nil).(*manager)
	manager.ctx, manager.cancel = context.WithCancel(context.Background())
	defer manager.Stop()
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	observed := &observedHandshakeRead{Conn: server, entered: make(chan struct{})}
	secured := tls.Server(observed, serverTLS)
	manager.wg.Add(1)
	go func() { defer manager.wg.Done(); manager.handleInboundConnection(secured) }()
	select {
	case <-observed.entered:
	case <-time.After(time.Second):
		t.Fatal("TLS handshake never reached socket read")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- manager.Stop() }()
	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(time.Second):
		_ = client.Close()
		<-stopped
		t.Fatal("manager Stop waited for peer or handshake timeout")
	}
	_, err = client.Write([]byte("late"))
	require.Error(t, err, "cancellation must release the underlying socket")
}

func TestOutboundIdentityHandshakeCancelsAfterTLS(t *testing.T) {
	a, b := authenticatedTestManagers(t, func(c *ManagerConfig) { c.HandshakeTimeout = time.Hour })
	clientManager := a.(*manager)
	serverManager := b.(*manager)
	var err error
	clientManager.tlsConfig, err = loadTLSConfig(clientManager.config.TLS)
	require.NoError(t, err)
	serverTLS, err := loadTLSConfig(serverManager.config.TLS)
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := make(chan error, 1)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		raw, err := listener.Accept()
		if err != nil {
			observed <- err
			return
		}
		defer raw.Close()
		secured := tls.Server(raw, serverTLS)
		if err := secured.HandshakeContext(ctx); err != nil {
			observed <- err
			return
		}
		// Read the start of the signed node handshake, then withhold the response.
		_, err = secured.Read(make([]byte, 1))
		observed <- err
		<-ctx.Done()
	}()
	loopCtx, stopLoop := context.WithCancel(context.Background())
	defer stopLoop()
	loop := &nodeControlLoop{nodeID: "node-b", manager: clientManager, ctx: loopCtx, commands: make(chan nodeCommand, 1), logger: zap.NewNop()}
	done := make(chan struct{})
	go func() { defer close(done); loop.attemptConnection("127.0.0.1", listener.Addr().(*net.TCPAddr).Port) }()
	defer func() { stopLoop(); cancel(); _ = listener.Close(); <-done; <-serverDone }()
	select {
	case err := <-observed:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("TLS/node handshake never reached stalled server")
	}
	stopLoop()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("outbound node handshake ignored loop cancellation")
	}
}
