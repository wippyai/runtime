// SPDX-License-Identifier: MPL-2.0
package net

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type packetRouteService struct {
	err     error
	network string
	address string
	mockService
	called bool
}

func (s *packetRouteService) ListenPacket(_ context.Context, network, address string) (net.PacketConn, error) {
	s.called = true
	s.network, s.address = network, address
	return nil, s.err
}

func TestSecureServiceListenPacketSelectedNetwork(t *testing.T) {
	expected := errors.New("overlay packet binding unavailable")
	overlay := &packetRouteService{err: expected}
	reg := NewRegistry(zap.NewNop())
	reg.Register(registry.ParseID("app.net:packets"), overlay, netapi.KindSOCKS5)
	ctx := netapi.WithNetworkRegistry(nonStrictCtx(), reg)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:packets")))
	conn, err := NewSecureService().ListenPacket(ctx, "udp", "127.0.0.1:0")
	if conn != nil {
		_ = conn.Close()
	}
	require.Nil(t, conn)
	require.ErrorIs(t, err, expected)
	require.True(t, overlay.called)
	require.Equal(t, "udp", overlay.network)
	require.Equal(t, "127.0.0.1:0", overlay.address)
}

func TestSecureServiceListenPacketNeverFallsBackFromMissingNetwork(t *testing.T) {
	for _, withRegistry := range []bool{false, true} {
		ctx := nonStrictCtx()
		if withRegistry {
			ctx = netapi.WithNetworkRegistry(ctx, NewRegistry(zap.NewNop()))
		}
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:missing")))
		conn, err := NewSecureService().ListenPacket(ctx, "udp", "127.0.0.1:0")
		if conn != nil {
			_ = conn.Close()
		}
		require.Nil(t, conn)
		require.Error(t, err)
	}
}
