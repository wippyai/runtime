// SPDX-License-Identifier: MPL-2.0

package wasm

import wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"

// wasi1HostProfile provides compatibility mapping for core WASI imports.
// Registration is handled by wasm-runtime for core modules.
func wasi1HostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name: wasmcomponent.HostProfileWASI1,
		Aliases: []string{
			wasmcomponent.HostProfileWASI1,
			"wasi-preview1",
			"preview1",
			"wasi_snapshot_preview1",
		},
	}
}
