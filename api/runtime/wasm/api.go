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
)

const (
	// DefaultMaxSize defines default elastic pool max workers.
	DefaultMaxSize = 100
)

// Security inheritance defaults for WASM executions.
// These are runtime-managed and not configurable per entry.
const (
	DefaultInheritActor          = true
	DefaultInheritScope          = true
	DefaultInheritRequestContext = true
)

// Pool type constants for scheduler implementation selection.
const (
	PoolTypeLazy     = "lazy"     // Zero idle workers, scale on demand.
	PoolTypeStatic   = "static"   // Fixed worker pool.
	PoolTypeInline   = "inline"   // Synchronous inline execution.
	PoolTypeAdaptive = "adaptive" // Auto-scaling worker pool.
)

// Transport type constants for input/output mapping.
const (
	TransportTypePayload  = "payload"
	TransportTypeWASIHTTP = "wasi-http"
)

// Host class constants used for capability gating in deterministic contexts.
const (
	ClassDeterministic    = "deterministic"
	ClassNondeterministic = "nondeterministic"
	ClassIO               = "io"
	ClassNetwork          = "network"
	ClassTime             = "time"
	ClassStorage          = "storage"
	ClassProcess          = "process"
	ClassSecurity         = "security"
)
