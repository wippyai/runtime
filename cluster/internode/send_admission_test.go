// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestOrdinarySendRefusesUnknownAndDepartedPeer(t *testing.T) {
	for _, departed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "departed"}[departed], func(t *testing.T) {
			cfg := insecureManagerConfig()
			cfg.Logger = zap.NewNop()
			m := NewConnectionManager(cfg, nil).(*manager)
			membership := &mockMembership{}
			if departed {
				m.AddManagedNode("peer")
				m.RemoveManagedNode("peer")
				membership.nodes = []cluster.NodeInfo{{ID: "peer"}} // deliberately stale discovery view
			}
			service := NewService(zap.NewNop(), m, &mockCodec{encoded: []byte("notice")}, nil, nil, membership)
			pkg := relay.NewServicePackage("local", "host", "peer", "remote", "request")
			lease := &deliveryReleaseProbe{}
			pkg.Messages[0].SetRetentionLease(lease)
			err := service.Send(pkg)
			// If the old implementation consumed it, do not return it to the pool twice.
			defer func() {
				if lease.releases == 0 {
					relay.ReleasePackage(pkg)
				}
			}()
			require.ErrorIs(t, err, ErrNodeNotManaged)
			require.Zero(t, lease.releases)
			require.False(t, m.IsManaged("peer"), "a send cannot re-enroll a departed peer from stale gossip")
			m.AddManagedNode("peer") // only the transport owner restores membership
			require.NoError(t, service.Send(pkg))
			require.Equal(t, 1, lease.releases)
			require.Equal(t, [][]byte{[]byte("notice")}, drainAllData(m.nodeStates, "peer"))
			m.RemoveManagedNode("peer")
		})
	}
}
