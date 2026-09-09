// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// NewSurfaceTransport shares the native surface protocol with compiled hosts
// that assemble a mesh without the full boot graph. It opens no listener and
// registers its class receiver only when the TTY service calls Receive. The host
// must close its TTY service before stopping the supplied connection manager.
// Receiver registration is exclusive for the manager's lifetime.
func NewSurfaceTransport(manager ConnectionManager, membership clusterapi.Membership) (ttyapi.MeshTransport, error) {
	if manager == nil || membership == nil {
		return nil, ttyapi.ErrServiceUnavailable
	}
	return surfaceTransport{cm: manager, membership: membership}, nil
}

type surfaceTransport struct {
	cm         ConnectionManager
	membership clusterapi.Membership
}

func (t surfaceTransport) Send(peer string, data []byte) error {
	return t.cm.SendToNode(peer, data, ClassSurface)
}
func (t surfaceTransport) Receive(fn func(string, []byte)) error {
	if !t.cm.RegisterClassReceiver(ClassSurface, fn) {
		return errors.New("terminal mesh receiver already registered")
	}
	return nil
}

func (t surfaceTransport) CheckPeer(peer string) error {
	if t.membership != nil {
		for _, node := range t.membership.Nodes() {
			if node.ID == peer && node.Meta[MetadataSurfaceProtocol] == "1" {
				return nil
			}
		}
	}
	return ttyapi.ErrServiceUnavailable
}
