// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"errors"

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
			return ctx, service.SetMesh(node.ID(), surfaceMesh{cm: cm, membership: clusterapi.GetMembership(ctx)})
		},
	})
}

type surfaceMesh struct {
	cm         internode.ConnectionManager
	membership clusterapi.Membership
}

func (t surfaceMesh) Send(peer string, data []byte) error {
	return t.cm.SendToNode(peer, data, internode.ClassSurface)
}
func (t surfaceMesh) Receive(fn func(string, []byte)) error {
	if !t.cm.RegisterClassReceiver(internode.ClassSurface, fn) {
		return errors.New("terminal mesh receiver already registered")
	}
	return nil
}

func (t surfaceMesh) CheckPeer(peer string) error {
	if t.membership != nil {
		for _, node := range t.membership.Nodes() {
			if node.ID == peer && node.Meta[internode.MetadataSurfaceProtocol] == "1" {
				return nil
			}
		}
	}
	return ttyapi.ErrServiceUnavailable
}
