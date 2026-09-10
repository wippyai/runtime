// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/require"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/cluster/internode"
)

type surfaceMembership []clusterapi.NodeInfo

func (m surfaceMembership) Nodes() []clusterapi.NodeInfo { return m }
func (surfaceMembership) LocalNode() clusterapi.NodeInfo { return clusterapi.NodeInfo{ID: "local"} }
func (surfaceMembership) UpdateMeta(map[string]string)   {}

func TestSurfaceMeshChecksPeerProtocolBeforeAttach(t *testing.T) {
	transport := surfaceMesh{membership: surfaceMembership{
		{ID: "old"},
		{ID: "new", Meta: clusterapi.NodeMeta{internode.MetadataSurfaceProtocol: "1"}},
	}}
	require.NoError(t, transport.CheckPeer("new"))
	require.ErrorIs(t, transport.CheckPeer("old"), ttyapi.ErrServiceUnavailable)
	require.ErrorIs(t, transport.CheckPeer("absent"), ttyapi.ErrServiceUnavailable)
	require.ErrorIs(t, (surfaceMesh{}).CheckPeer("new"), ttyapi.ErrServiceUnavailable)
}
