// SPDX-License-Identifier: MPL-2.0

// Package wasm provides WASM runtime integration types.
package wasm

import (
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

const (
	// System is the event system used by WASM runtime components.
	System event.System = "wasm"
	// InvalidateNodes is sent when WASM function code should be reloaded.
	InvalidateNodes event.Kind = "wasm.reset_code"
)

// Registry kind constants for WASM function component types.
const (
	// FunctionWAT identifies an inline WAT function component.
	FunctionWAT registry.Kind = "function.wat"
	// FunctionWASM identifies a precompiled WASM function component loaded from FS.
	FunctionWASM registry.Kind = "function.wasm"
	// ProcessWASM identifies a precompiled WASM process component loaded from FS.
	ProcessWASM registry.Kind = "process.wasm"
)

const (
	// DefaultMaxSize defines default elastic pool max workers.
	DefaultMaxSize                           = 100
	DefaultMaxRetainedMemoryBytes      int64 = 64 * 1024 * 1024
	DefaultRetainedMemoryCheckInterval       = 16
	DefaultMaxOpenSockets                    = 16
	DefaultSocketTimeoutMS                   = 30_000
)

// Pool type constants for scheduler implementation selection.
const (
	PoolTypeLazy     = "lazy"     // Zero idle workers, scale on demand.
	PoolTypeStatic   = "static"   // Fixed worker pool.
	PoolTypeInline   = "inline"   // Synchronous inline execution.
	PoolTypeAdaptive = "adaptive" // Auto-scaling worker pool.
)

// Worker-class constants. A pool's worker_class selects a worker-isolation
// strategy independent of the pool type, and names a scheduler worker class
// (mirrors the v2 runtime). Any non-empty name runs the pool on a dedicated,
// OS-thread-pinned set of workers.
const (
	// WorkerClassWASM is the conventional class name for CPU-bound WASM work,
	// keeping it off the actor-scheduler workers.
	WorkerClassWASM = "wasm"
)

// Transport type constants for input/output mapping.
const (
	TransportTypePayload  = "payload"
	TransportTypeWASIHTTP = "wasi-http"
)
