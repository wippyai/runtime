// SPDX-License-Identifier: MPL-2.0
package net

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type dnsRouteService struct {
	err  error
	host string
	mockService
	called bool
}

func (s *dnsRouteService) LookupHost(_ context.Context, host string) ([]string, error) {
	s.called = true
	s.host = host
	if s.err != nil {
		return nil, s.err
	}
	return []string{"192.0.2.1"}, nil
}

func TestSecureServiceLookupHostSelectedNetwork(t *testing.T) {
	for _, lookupErr := range []error{nil, errors.New("overlay DNS unavailable")} {
		overlay := &dnsRouteService{err: lookupErr}
		reg := NewRegistry(zap.NewNop())
		reg.Register(registry.ParseID("app.net:dns"), overlay, netapi.KindSOCKS5)
		ctx := netapi.WithNetworkRegistry(nonStrictCtx(), reg)
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:dns")))
		addresses, err := NewSecureService().LookupHost(ctx, "example.invalid")
		require.ErrorIs(t, err, lookupErr)
		require.True(t, overlay.called)
		require.Equal(t, "example.invalid", overlay.host)
		if err == nil {
			require.Equal(t, []string{"192.0.2.1"}, addresses)
		}
		require.NoError(t, frame.Close())
	}
}

func TestSecureServiceLookupHostNoFallbackForMissingNetwork(t *testing.T) {
	for _, registered := range []bool{false, true} {
		ctx := nonStrictCtx()
		if registered {
			ctx = netapi.WithNetworkRegistry(ctx, NewRegistry(zap.NewNop()))
		}
		ctx, frame := ctxapi.OpenFrameContext(ctx)
		require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:missing")))
		addresses, err := NewSecureService().LookupHost(ctx, "localhost")
		require.Nil(t, addresses)
		require.Error(t, err)
		require.NoError(t, frame.Close())
	}
}

func TestSecureServiceLookupHostChecksPermissionBeforeOverlay(t *testing.T) {
	overlay := &dnsRouteService{}
	reg := NewRegistry(zap.NewNop())
	reg.Register(registry.ParseID("app.net:dns"), overlay, netapi.KindSOCKS5)
	ctx := netapi.WithNetworkRegistry(strictCtx(), reg)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	defer frame.Close()
	require.NoError(t, frame.SetMultiple(netapi.DefaultNetworkPair("app.net:dns")))
	addresses, err := NewSecureService().LookupHost(ctx, "localhost")
	require.Nil(t, addresses)
	require.ErrorIs(t, err, netapi.ErrAccessDenied)
	require.False(t, overlay.called)
}
