// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	netservice "github.com/wippyai/runtime/service/net"
	netsystem "github.com/wippyai/runtime/system/net"
)

// Network creates the network overlay service boot component.
// It creates the driver manager that handles Tor, I2P, and Tailscale
// overlay entries from the registry, delegating storage to the
// system-level network Registry.
func Network() boot.Component {
	return boot.New(boot.P{
		Name:      NetworkServiceName,
		DependsOn: []boot.Name{bootsystem.NetworkName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			// Retrieve the system-level registry.
			netReg := netapi.GetNetworkRegistry(ctx)
			if netReg == nil {
				return ctx, fmt.Errorf("network service: system registry not found")
			}
			reg, ok := netReg.(*netsystem.Registry)
			if !ok {
				return ctx, fmt.Errorf("network service: unexpected registry type %T", netReg)
			}

			manager, err := netservice.NewManager(reg, dtt, logger.Named("network"))
			if err != nil {
				return ctx, err
			}

			handlers.RegisterListener("network.*", manager)
			return ctx, nil
		},
	})
}
