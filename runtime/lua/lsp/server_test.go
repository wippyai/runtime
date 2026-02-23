// SPDX-License-Identifier: MPL-2.0

package lsp

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/lsp/transport"
	"go.uber.org/zap"
)

func listenTCP(t *testing.T) net.Listener {
	t.Helper()
	lc := net.ListenConfig{}
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return l
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	d := net.Dialer{}
	conn, err := d.DialContext(context.Background(), "tcp", addr)
	require.NoError(t, err)
	return conn
}

func TestResponseError_Codes(t *testing.T) {
	tests := []struct {
		name string
		code int
	}{
		{"ParseError", transport.ParseError},
		{"InvalidRequest", transport.InvalidRequest},
		{"MethodNotFound", transport.MethodNotFound},
		{"InvalidParams", transport.InvalidParams},
		{"InternalError", transport.InternalError},
		{"ServerNotInitialized", transport.ServerNotInitialized},
		{"RequestCancelled", transport.RequestCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Less(t, tt.code, 0, "%s should be negative", tt.name)
		})
	}
}

func TestServer_StartStopTCP(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx := context.Background()
	var err error

	err = server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StartTwice(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Address: "127.0.0.1:0",
	}
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx := context.Background()
	var err error

	err = server.Start(ctx)
	require.NoError(t, err)

	err = server.Start(ctx)
	require.NoError(t, err)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_StopWithoutStart(t *testing.T) {
	cfg := DefaultConfig()
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	err := server.Stop()
	require.NoError(t, err)
}

func TestServer_TCPConnection(t *testing.T) {
	listener := listenTCP(t)
	defer listener.Close()

	cfg := Config{
		Enabled: true,
		Address: listener.Addr().String(),
	}
	listener.Close()

	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	conn := dialTCP(t, cfg.Address)
	defer conn.Close()

	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(req), req)

	_, err = conn.Write([]byte(msg))
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}

func TestServer_MultipleConnections(t *testing.T) {
	listener := listenTCP(t)
	addr := listener.Addr().String()
	listener.Close()

	cfg := Config{
		Enabled: true,
		Address: addr,
	}
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{}
			conn, err := d.DialContext(context.Background(), "tcp", cfg.Address)
			if err != nil {
				return
			}
			defer conn.Close()
			time.Sleep(50 * time.Millisecond)
		}()
	}

	time.Sleep(100 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)

	wg.Wait()
}

func TestServer_ConnectionCleanupOnStop(t *testing.T) {
	listener := listenTCP(t)
	addr := listener.Addr().String()
	listener.Close()

	cfg := Config{
		Enabled: true,
		Address: addr,
	}
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := server.Start(ctx)
	require.NoError(t, err)

	conns := make([]net.Conn, 5)
	for i := 0; i < 5; i++ {
		conn := dialTCP(t, cfg.Address)
		conns[i] = conn
	}

	time.Sleep(50 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)

	for _, conn := range conns {
		conn.Close()
	}
}

func TestServer_ContextCancellation(t *testing.T) {
	listener := listenTCP(t)
	addr := listener.Addr().String()
	listener.Close()

	cfg := Config{
		Enabled: true,
		Address: addr,
	}
	log := zap.NewNop()
	svc := &Service{}

	server := transport.NewServer(cfg.Address, log, svc, cfg.MaxMessageBytes)

	ctx, cancel := context.WithCancel(context.Background())

	err := server.Start(ctx)
	require.NoError(t, err)

	conn := dialTCP(t, cfg.Address)
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	cancel()

	time.Sleep(200 * time.Millisecond)

	err = server.Stop()
	require.NoError(t, err)
}
