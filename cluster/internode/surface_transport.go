// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"errors"
	"sync"

	clusterapi "github.com/wippyai/runtime/api/cluster"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// NewSurfaceTransport shares the native surface protocol with compiled hosts
// that assemble a mesh without the full boot graph. It opens no listener and
// registers its class receiver only when the TTY service calls Receive. The host
// must close its TTY service before stopping the supplied connection manager.
// Each adapter owns at most one registration lifetime. Receive(nil) releases
// only its own successful registration and permanently retires that adapter.
// A replacement service constructs a fresh adapter. The host must not manipulate
// ClassSurface registration directly while an adapter owns it. Release does not
// drain callbacks already dispatched; the TTY service fences them during close.
func NewSurfaceTransport(manager ConnectionManager, membership clusterapi.Membership) (ttyapi.MeshTransport, error) {
	if manager == nil || membership == nil {
		return nil, ttyapi.ErrServiceUnavailable
	}
	return &surfaceTransport{cm: manager, membership: membership}, nil
}

type surfaceTransport struct {
	cm         ConnectionManager
	membership clusterapi.Membership
	mu         sync.Mutex
	claimed    bool
	retired    bool
}

func (t *surfaceTransport) Send(peer string, data []byte) error {
	err := t.cm.SendToNode(peer, data, ClassSurface)
	if errors.Is(err, ErrQueueFull) {
		return ttyapi.ErrMeshBusy
	}
	return err
}
func (t *surfaceTransport) Receive(fn func(string, []byte)) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if fn == nil {
		if t.retired {
			return nil
		}
		if t.claimed {
			if !t.cm.RegisterClassReceiver(ClassSurface, nil) {
				return errors.New("terminal mesh receiver release failed")
			}
			t.claimed = false
		}
		t.retired = true
		return nil
	}
	if t.retired {
		return ttyapi.ErrServiceUnavailable
	}
	if t.claimed || !t.cm.RegisterClassReceiver(ClassSurface, fn) {
		return errors.New("terminal mesh receiver already registered")
	}
	t.claimed = true
	return nil
}

func (t *surfaceTransport) CheckPeer(peer string) error {
	if t.membership != nil {
		for _, node := range t.membership.Nodes() {
			if node.ID == peer && node.Meta[MetadataSurfaceProtocol] == "1" {
				return nil
			}
		}
	}
	return ttyapi.ErrServiceUnavailable
}

func (t *surfaceTransport) SupportsGraphics(peer string) bool {
	if t.membership != nil {
		for _, node := range t.membership.Nodes() {
			if node.ID == peer {
				return node.Meta[MetadataSurfaceProtocol] == "1" && node.Meta[MetadataSurfaceGraphics] == "1"
			}
		}
	}
	return false
}
