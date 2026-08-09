// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

type reentrantCloseService struct {
	registry *Registry
	observed chan bool
	id       registry.ID
}

func (s *reentrantCloseService) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}
func (s *reentrantCloseService) Listen(context.Context, string, string) (net.Listener, error) {
	return nil, nil
}
func (s *reentrantCloseService) ListenPacket(context.Context, string, string) (net.PacketConn, error) {
	return nil, nil
}
func (s *reentrantCloseService) LookupHost(context.Context, string) ([]string, error) {
	return nil, nil
}
func (s *reentrantCloseService) Close() error {
	s.observed <- s.registry.HasNetwork(s.id)
	_, err := s.registry.GetNetwork(s.id)
	s.observed <- errors.Is(err, netapi.ErrNetworkNotFound)
	return nil
}

func TestY06NetworkUnregisterCloseReentrant(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	id := registry.ParseID("network:reentrant")
	observed := make(chan bool, 2)
	reg.Register(id, &reentrantCloseService{registry: reg, id: id, observed: observed}, netapi.KindSOCKS5)

	reg.Unregister(id)

	require.False(t, <-observed, "entry must be absent before Close re-enters the registry")
	require.True(t, <-observed)
	require.False(t, reg.HasNetwork(id))
}
