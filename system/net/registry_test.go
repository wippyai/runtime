// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

// mockService implements netapi.Service for testing.
type mockService struct {
	dialCalled bool
}

func (m *mockService) DialContext(_ context.Context, _, _ string) (net.Conn, error) {
	m.dialCalled = true
	return nil, nil
}
func (m *mockService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, nil
}
func (m *mockService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, nil
}
func (m *mockService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// mockClosableService implements netapi.Service + io.Closer.
type mockClosableService struct {
	closeErr error
	mockService
	closeMu sync.Mutex
	closed  bool
}

func (m *mockClosableService) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	m.closed = true
	return m.closeErr
}

func (m *mockClosableService) wasClosed() bool {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.closed
}

func TestRegistry_ImplementsInterface(t *testing.T) {
	var _ netapi.NetworkRegistry = (*Registry)(nil)
}

func TestRegistry_NilLogger(t *testing.T) {
	reg := NewRegistry(nil)
	require.NotNil(t, reg)
	// Should not panic with nil logger
	reg.Register(registry.ParseID("network:test"), &mockService{}, netapi.KindSOCKS5)
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	svc := &mockService{}
	id := registry.ParseID("network:my-socks5")

	reg.Register(id, svc, netapi.KindSOCKS5)

	got, err := reg.GetNetwork(id)
	require.NoError(t, err)
	assert.Equal(t, svc, got)
}

func TestRegistry_HasNetwork(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	id := registry.ParseID("network:test-net")

	assert.False(t, reg.HasNetwork(id))

	reg.Register(id, &mockService{}, netapi.KindI2P)
	assert.True(t, reg.HasNetwork(id))
}

func TestRegistry_NetworkKind(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	id := registry.ParseID("network:my-tailscale")

	// Not registered yet
	assert.Equal(t, registry.Kind(""), reg.NetworkKind(id))

	reg.Register(id, &mockService{}, netapi.KindTailscale)
	assert.Equal(t, netapi.KindTailscale, reg.NetworkKind(id))
}

func TestRegistry_GetNetwork_NotFound(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	id := registry.ParseID("network:nonexistent")

	svc, err := reg.GetNetwork(id)
	assert.Nil(t, svc)
	require.ErrorIs(t, err, netapi.ErrNetworkNotFound)
}

func TestRegistry_Unregister(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	svc := &mockService{}
	id := registry.ParseID("network:removeme")

	reg.Register(id, svc, netapi.KindSOCKS5)
	require.True(t, reg.HasNetwork(id))

	reg.Unregister(id)
	assert.False(t, reg.HasNetwork(id))

	_, err := reg.GetNetwork(id)
	require.ErrorIs(t, err, netapi.ErrNetworkNotFound)
}

func TestRegistry_Unregister_ClosesService(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	svc := &mockClosableService{}
	id := registry.ParseID("network:closable")

	reg.Register(id, svc, netapi.KindTailscale)
	reg.Unregister(id)

	assert.True(t, svc.wasClosed(), "service should be closed on unregister")
}

func TestRegistry_Unregister_CloseError(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	svc := &mockClosableService{closeErr: errors.New("close failed")}
	id := registry.ParseID("network:close-err")

	reg.Register(id, svc, netapi.KindTailscale)
	// Should not panic even if Close() returns error
	reg.Unregister(id)

	assert.True(t, svc.wasClosed())
	assert.False(t, reg.HasNetwork(id))
}

func TestRegistry_Unregister_NonClosable(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	svc := &mockService{} // does NOT implement io.Closer
	id := registry.ParseID("network:no-close")

	reg.Register(id, svc, netapi.KindSOCKS5)
	// Should not panic when service doesn't implement Closer
	reg.Unregister(id)
	assert.False(t, reg.HasNetwork(id))
}

func TestRegistry_Unregister_NotFound(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	// Should not panic when unregistering something that doesn't exist
	reg.Unregister(registry.ParseID("network:ghost"))
}

func TestRegistry_UpdateReplacesService(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	id := registry.ParseID("network:update-me")

	svc1 := &mockClosableService{}
	svc2 := &mockService{}

	reg.Register(id, svc1, netapi.KindSOCKS5)

	got1, err := reg.GetNetwork(id)
	require.NoError(t, err)
	assert.Equal(t, svc1, got1)

	// Re-register with new service (note: Registry.Register replaces directly,
	// caller is responsible for closing old service — this tests that behavior)
	reg.Register(id, svc2, netapi.KindI2P)

	got2, err := reg.GetNetwork(id)
	require.NoError(t, err)
	assert.Equal(t, svc2, got2)
	assert.Equal(t, netapi.KindI2P, reg.NetworkKind(id))
}

func TestRegistry_MultipleNetworks(t *testing.T) {
	reg := NewRegistry(zap.NewNop())

	socksID := registry.ParseID("network:socks1")
	i2pID := registry.ParseID("network:i2p1")
	tsID := registry.ParseID("network:ts1")

	socksSvc := &mockService{}
	i2pSvc := &mockService{}
	tsSvc := &mockService{}

	reg.Register(socksID, socksSvc, netapi.KindSOCKS5)
	reg.Register(i2pID, i2pSvc, netapi.KindI2P)
	reg.Register(tsID, tsSvc, netapi.KindTailscale)

	got, err := reg.GetNetwork(socksID)
	require.NoError(t, err)
	assert.Equal(t, socksSvc, got)

	got, err = reg.GetNetwork(i2pID)
	require.NoError(t, err)
	assert.Equal(t, i2pSvc, got)

	got, err = reg.GetNetwork(tsID)
	require.NoError(t, err)
	assert.Equal(t, tsSvc, got)

	assert.Equal(t, netapi.KindSOCKS5, reg.NetworkKind(socksID))
	assert.Equal(t, netapi.KindI2P, reg.NetworkKind(i2pID))
	assert.Equal(t, netapi.KindTailscale, reg.NetworkKind(tsID))
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry(zap.NewNop())
	const goroutines = 100
	const iterations = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := registry.ParseID("network:concurrent")

			for j := 0; j < iterations; j++ {
				svc := &mockClosableService{}

				// Mix of operations
				switch j % 4 {
				case 0:
					reg.Register(id, svc, netapi.KindSOCKS5)
				case 1:
					reg.GetNetwork(id)
				case 2:
					reg.HasNetwork(id)
				case 3:
					reg.Unregister(id)
				}
			}
		}(i)
	}

	wg.Wait()
}
