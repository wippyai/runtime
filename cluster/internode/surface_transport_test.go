// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"testing"

	"github.com/stretchr/testify/require"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type surfaceMembership []clusterapi.NodeInfo

func (m surfaceMembership) Nodes() []clusterapi.NodeInfo { return m }
func (surfaceMembership) LocalNode() clusterapi.NodeInfo { return clusterapi.NodeInfo{ID: "local"} }
func (surfaceMembership) UpdateMeta(map[string]string)   {}

func TestSurfaceMeshChecksPeerProtocolBeforeAttach(t *testing.T) {
	transport := surfaceTransport{membership: surfaceMembership{
		{ID: "old"},
		{ID: "new", Meta: clusterapi.NodeMeta{MetadataSurfaceProtocol: "1"}},
	}}
	require.NoError(t, transport.CheckPeer("new"))
	require.ErrorIs(t, transport.CheckPeer("old"), ttyapi.ErrServiceUnavailable)
	require.ErrorIs(t, transport.CheckPeer("absent"), ttyapi.ErrServiceUnavailable)
	require.ErrorIs(t, (&surfaceTransport{}).CheckPeer("new"), ttyapi.ErrServiceUnavailable)
}

func TestSurfaceTransportRequiresHostOwnedMesh(t *testing.T) {
	transport, err := NewSurfaceTransport(nil, surfaceMembership{})
	require.Nil(t, transport)
	require.ErrorIs(t, err, ttyapi.ErrServiceUnavailable)
}

func TestSurfaceGraphicsCapabilityIsAdditive(t *testing.T) {
	transport := surfaceTransport{membership: surfaceMembership{
		{ID: "text", Meta: clusterapi.NodeMeta{MetadataSurfaceProtocol: "1"}},
		{ID: "images", Meta: clusterapi.NodeMeta{MetadataSurfaceProtocol: "1", MetadataSurfaceGraphics: "1"}},
		{ID: "wrong", Meta: clusterapi.NodeMeta{MetadataSurfaceProtocol: "2", MetadataSurfaceGraphics: "1"}},
	}}
	require.NoError(t, transport.CheckPeer("text"))
	require.NoError(t, transport.CheckPeer("images"))
	require.False(t, transport.SupportsGraphics("text"))
	require.True(t, transport.SupportsGraphics("images"))
	require.False(t, transport.SupportsGraphics("wrong"))
	require.False(t, transport.SupportsGraphics("absent"))
}

func TestSurfaceTransportReleaseOwnsOnlyItsSuccessfulRegistration(t *testing.T) {
	manager := &manager{}
	membership := surfaceMembership{}
	first, err := NewSurfaceTransport(manager, membership)
	require.NoError(t, err)
	second, err := NewSurfaceTransport(manager, membership)
	require.NoError(t, err)
	calls := 0
	require.NoError(t, first.Receive(func(string, []byte) { calls++ }))
	require.Error(t, second.Receive(func(string, []byte) { t.Error("failed receiver invoked") }))
	require.NoError(t, second.Receive(nil), "failed admission cleanup must not clear the first owner")
	receive := manager.lookupClassReceiver(ClassSurface)
	require.NotNil(t, receive)
	receive("peer", nil)
	require.Equal(t, 1, calls)
	require.NoError(t, first.Receive(nil))
	replacement, err := NewSurfaceTransport(manager, membership)
	require.NoError(t, err)
	require.NoError(t, replacement.Receive(func(string, []byte) { calls += 10 }))
	require.NoError(t, first.Receive(nil), "retired cleanup must not clear its replacement")
	completed := make(chan error, 16)
	for range 16 {
		go func() { completed <- first.Receive(nil) }()
	}
	for range 16 {
		require.NoError(t, <-completed)
	}
	receive = manager.lookupClassReceiver(ClassSurface)
	require.NotNil(t, receive)
	receive("peer", nil)
	require.Equal(t, 11, calls)
	require.Error(t, first.Receive(func(string, []byte) {}), "retired adapter cannot claim a new lifetime")
	require.NoError(t, replacement.Receive(nil))
}
