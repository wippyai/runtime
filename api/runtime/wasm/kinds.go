// Package wasm provides WASM runtime integration.
package wasm

import "github.com/wippyai/runtime/api/registry"

// Registry kind constants for WASM component types.
const (
	// KindFunction identifies a WASM function component with inline WAT source
	KindFunction registry.Kind = "function.wat"
)
