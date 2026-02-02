package wasm

import "github.com/wippyai/runtime/api/registry"

// Registry kind constants for WASM component types.
const (
	// FunctionWAT identifies a WASM function with inline WAT source.
	FunctionWAT registry.Kind = "function.wat"

	// FunctionWASM identifies a precompiled WASM binary loaded from filesystem.
	FunctionWASM registry.Kind = "function.wasm"

	// FunctionComponent identifies a WebAssembly Component Model binary.
	FunctionComponent registry.Kind = "function.component"

	// ProcessWASM identifies a long-running WASM process.
	ProcessWASM registry.Kind = "process.wasm"

	// ProcessComponent identifies a long-running Component Model process.
	ProcessComponent registry.Kind = "process.component"
)
