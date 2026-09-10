// SPDX-License-Identifier: MPL-2.0

package process

import (
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
)

// RegisterHostProfiles configures host profiles used by wasm process modules.
func (m *Manager) RegisterHostProfiles(profiles ...wasmcomponent.HostProfile) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.hostRegistry.RegisterProfiles(profiles...)
}
