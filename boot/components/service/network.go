// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	netservice "github.com/wippyai/runtime/service/net"
)

// Network creates the network overlay service boot component.
// It manages lifecycle of overlay network entries (Tor, I2P, Tailscale, etc.)
// and provides a NetworkRegistry for consumers like the HTTP client dispatcher.
func Network() boot.Component {
	return boot.New(boot.P{
		Name:      NetworkServiceName,
		DependsOn: []boot.Name{bootsystem.NetworkName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager, err := netservice.NewManager(dtt, logger.Named("network"))
			if err != nil {
				return ctx, err
			}

			handlers.RegisterListener("network.*", manager)
			ctx = netapi.WithNetworkRegistry(ctx, manager)
			return ctx, nil
		},
	})
}
