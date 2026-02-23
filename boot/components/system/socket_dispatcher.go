// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	socketsvc "github.com/wippyai/runtime/service/socket"
)

// SocketDispatcher creates the socket dispatcher component.
// Placed in system package because it depends on the Network service.
func SocketDispatcher() boot.Component {
	return boot.New(boot.P{
		Name:      dispatchers.SocketDispatcherName,
		DependsOn: []boot.Name{dispatchers.DispatcherName, NetworkName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, dispatchers.ErrDispatcherNotFound
			}

			netSvc := netapi.GetService(ctx)
			if netSvc == nil {
				return ctx, dispatchers.ErrNetServiceNotFound
			}

			d := socketsvc.NewDispatcher(netSvc)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
