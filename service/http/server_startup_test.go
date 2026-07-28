// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	config "github.com/wippyai/runtime/api/service/http"
	"go.uber.org/zap"
)

type failingProbeService struct {
	listened chan struct{}
}

func (s *failingProbeService) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("injected probe failure")
}

func (s *failingProbeService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err == nil {
		close(s.listened)
	}
	return ln, err
}

func (s *failingProbeService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return (&net.ListenConfig{}).ListenPacket(ctx, network, address)
}

func (s *failingProbeService) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func TestN01ServerProbeFailureRollsBack(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := reserved.Addr().String()
	require.NoError(t, reserved.Close())

	networkID := registry.NewID("test", "startup-probe")
	svc := &failingProbeService{listened: make(chan struct{})}
	reg := newMockNetRegistry()
	reg.register(networkID.String(), svc, netapi.KindI2P)

	server, err := NewServerService(
		registry.NewID("test", "startup-rollback"),
		&config.ServerConfig{Addr: addr, Network: networkID},
		NewMiddlewareRegistry(zap.NewNop()),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(overlayCtxWithRegistry(reg))
	startDone := make(chan error, 1)
	go func() {
		_, err := server.Start(ctx)
		startDone <- err
	}()

	select {
	case <-svc.listened:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("listener was not created")
	}

	select {
	case err := <-startDone:
		require.Error(t, err)
		require.Contains(t, err.Error(), "startup")
	case <-time.After(time.Second):
		t.Fatal("Start did not return after probe cancellation")
	}
	defer func() { _ = server.Stop(context.Background()) }()

	require.False(t, server.started.Load(), "failed startup must reset started state")
	require.Nil(t, server.server, "failed startup must discard the server")

	rebound, err := net.Listen("tcp", addr)
	require.NoError(t, err, "failed startup left its listener bound")
	require.NoError(t, rebound.Close())
}
