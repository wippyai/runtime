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
	require.ErrorIs(t, (surfaceTransport{}).CheckPeer("new"), ttyapi.ErrServiceUnavailable)
}

func TestSurfaceTransportRequiresHostOwnedMesh(t *testing.T) {
	transport, err := NewSurfaceTransport(nil, surfaceMembership{})
	require.Nil(t, transport)
	require.ErrorIs(t, err, ttyapi.ErrServiceUnavailable)
}

func TestSurfaceGraphicsCapabilityIsAdditive(t *testing.T) {
	transport := surfaceMesh{membership: surfaceMembership{
		{ID: "text", Meta: clusterapi.NodeMeta{internode.MetadataSurfaceProtocol: "1"}},
		{ID: "images", Meta: clusterapi.NodeMeta{internode.MetadataSurfaceProtocol: "1", internode.MetadataSurfaceGraphics: "1"}},
		{ID: "wrong", Meta: clusterapi.NodeMeta{internode.MetadataSurfaceProtocol: "2", internode.MetadataSurfaceGraphics: "1"}},
	}}
	require.NoError(t, transport.CheckPeer("text"))
	require.NoError(t, transport.CheckPeer("images"))
	require.False(t, transport.SupportsGraphics("text"))
	require.True(t, transport.SupportsGraphics("images"))
	require.False(t, transport.SupportsGraphics("wrong"))
	require.False(t, transport.SupportsGraphics("absent"))
}
