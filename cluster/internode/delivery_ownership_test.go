// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

type deliveryReleaseProbe struct{ releases int }

func (p *deliveryReleaseProbe) Release() { p.releases++ }

type deliveryLeaseCodec struct {
	lease *deliveryReleaseProbe
	mockCodec
}

func (c *deliveryLeaseCodec) Decode(data []byte) (*relay.Package, error) {
	pkg, err := c.mockCodec.Decode(data)
	if err != nil {
		return nil, err
	}
	message := relay.AcquireMessage()
	message.SetRetentionLease(c.lease)
	pkg.Messages = append(pkg.Messages, message)
	return pkg, nil
}

func TestServiceIncomingPackageOwnership(t *testing.T) {
	for _, reject := range []bool{true, false} {
		name := "accepted"
		if reject {
			name = "rejected"
		}
		t.Run(name, func(t *testing.T) {
			probe := &deliveryReleaseProbe{}
			codec := &deliveryLeaseCodec{lease: probe}
			manager := newMockConnectionManager()
			var accepted *relay.Package
			service := NewService(zap.NewNop(), manager, codec, func(pkg *relay.Package) error {
				if reject {
					return errors.New("destination refused admission")
				}
				accepted = pkg
				return nil
			}, eventbus.NewBus(), &mockMembership{localNode: cluster.NodeInfo{ID: "local"}})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, service.Start(ctx))
			defer func() { require.NoError(t, service.Stop()) }()
			manager.onMessage("remote-node", []byte("data"))
			if reject {
				require.Equal(t, 1, probe.releases, "rejected delivery must release its retained payload")
			} else {
				require.Zero(t, probe.releases, "accepted delivery belongs to the receiver")
				require.NotNil(t, accepted)
				relay.ReleasePackage(accepted)
				require.Equal(t, 1, probe.releases)
			}
		})
	}
}
