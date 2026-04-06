// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	netsystem "github.com/wippyai/runtime/system/net"
)

// Network creates the unified network system boot component.
// It initializes the secure clearnet service and the overlay network manager
// (Tor, I2P, Tailscale) as a single system-level component.
func Network() boot.Component {
	return boot.New(boot.P{
		Name: NetworkName,
		Load: func(ctx context.Context) (context.Context, error) {
			// Clearnet service with security enforcement.
			svc := netsystem.NewSecureService()
			ctx = netapi.WithService(ctx, svc)

			// Overlay network manager.
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager, err := netsystem.NewManager(dtt, logger.Named("network"))
			if err != nil {
				return ctx, err
			}

			handlers.RegisterListener("network.*", manager)
			ctx = netapi.WithNetworkRegistry(ctx, manager)

			return ctx, nil
		},
	})
}
