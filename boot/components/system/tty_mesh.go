// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/relay"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/cluster/internode"
	ttysystem "github.com/wippyai/runtime/system/tty"
)

// TTYMesh adds the dedicated protocol only when clustering is configured.
// TTY itself remains usable in local and embedded runtimes without a mesh.
func TTYMesh() boot.Component {
	return boot.New(boot.P{
		Name:      "tty.mesh",
		DependsOn: []boot.Name{TTYName, ClusterName},
		Load: func(ctx context.Context) (context.Context, error) {
			ac := ctxapi.AppFromContext(ctx)
			if ac == nil {
				return ctx, nil
			}
			cm, _ := ac.Get(connMgrKey).(internode.ConnectionManager)
			if cm == nil {
				return ctx, nil
			}
			service, ok := ttyapi.GetService(ctx).(*ttysystem.Service)
			node := relay.GetNode(ctx)
			if !ok || node == nil {
				return nil, ttyapi.ErrServiceUnavailable
			}
			transport, err := internode.NewSurfaceTransport(cm, clusterapi.GetMembership(ctx))
			if err != nil {
				return nil, err
			}
			return ctx, service.SetMesh(node.ID(), transport)
		},
	})
}
