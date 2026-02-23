// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"

	"github.com/wippyai/runtime/api/registry"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

// RegisterHostProfiles configures host profiles used by wasm process modules.
func (m *Manager) RegisterHostProfiles(profiles ...wasmcomponent.HostProfile) error {
	return m.hostRegistry.RegisterProfiles(profiles...)
}

func (m *Manager) ensureImportHosts(ctx context.Context, imports []registry.ID, component bool) error {
	var rt *wasmrt.Runtime
	if component {
		rt = m.runtimeInstance(true)
	} else {
		rt = m.runtimeInstance(false)
	}
	return m.hostRegistry.EnsureImports(ctx, rt, imports, component)
}
