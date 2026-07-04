// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	netsystem "github.com/wippyai/runtime/system/net"
)

// Network creates the network system boot component.
// It initializes the secure clearnet service and the overlay network
// registry. Concrete overlay drivers (Tor, I2P, Tailscale) are
// registered at the service layer.
func Network() boot.Component {
	return boot.New(boot.P{
		Name:      NetworkName,
		DependsOn: []boot.Name{FrameResolversName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)

			// Clearnet service with security enforcement.
			svc := netsystem.NewSecureService()
			ctx = netapi.WithService(ctx, svc)

			// Overlay network registry (driver-agnostic lookup).
			reg := netsystem.NewRegistry(logger.Named("network"))
			ctx = netapi.WithNetworkRegistry(ctx, reg)

			// Apply the overlay selection generically at spawn time.
			if resolvers := ctxapi.FrameResolversFrom(ctx); resolvers != nil {
				if err := resolvers.Register("network", FrameResolverOrderNetwork, netapi.OverlayResolver()); err != nil {
					return nil, err
				}
			}

			return ctx, nil
		},
	})
}
