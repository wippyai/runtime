// SPDX-License-Identifier: MPL-2.0

package internode

import (
	"context"

	"github.com/wippyai/runtime/api/cluster"
)

// WaitManaged waits for registration without creating a peer or connecting it.
// Registration is not a delivery guarantee; admission must recheck peer state.
// The caller bounds the wait. Manager shutdown also wakes it.
func (m *manager) WaitManaged(ctx context.Context, nodeID cluster.NodeID) error {
	var stopped <-chan struct{}
	if m.ctx != nil {
		stopped = m.ctx.Done()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if m.ctx != nil {
			if err := m.ctx.Err(); err != nil {
				return err
			}
		}
		// Subscribe before reading state so a concurrent join cannot be missed.
		m.managedMu.Lock()
		if m.managedChanged == nil {
			m.managedChanged = make(chan struct{})
		}
		changed := m.managedChanged
		m.managedMu.Unlock()
		if m.IsManaged(nodeID) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stopped:
			return m.ctx.Err()
		case <-changed:
		}
	}
}

func (m *manager) notifyManagedChange() {
	m.managedMu.Lock()
	if m.managedChanged != nil {
		close(m.managedChanged)
		m.managedChanged = nil
	}
	m.managedMu.Unlock()
}
