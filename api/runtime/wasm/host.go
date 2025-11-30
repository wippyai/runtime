// Package wasm provides WASM runtime integration for wippy.
package wasm

import (
	"github.com/wippyai/runtime/api/dispatcher"
)

// Host class constants for consistent categorization (mirrors Lua classes)
const (
	ClassDeterministic    = "deterministic"    // Same input = same output
	ClassNondeterministic = "nondeterministic" // Output varies (time, random)
	ClassIO               = "io"               // External I/O operations
	ClassNetwork          = "network"          // Network operations
	ClassTime             = "time"             // Time-related
	ClassProcess          = "process"          // Process management
	ClassStorage          = "storage"          // Filesystem/storage operations
)

// HostInfo contains metadata about a WASM host module
type HostInfo struct {
	Namespace   string   // WIT namespace (e.g., "wippy:clock")
	Description string   // Human-readable description
	Class       []string // Tags using Class* constants
}

// Host is the interface for WASM host modules.
// Similar to Lua Module but for WebAssembly host functions.
type Host interface {
	// Info returns host metadata including namespace and classification.
	Info() HostInfo

	// Register returns the host registration with functions and yield types.
	Register() *HostRegistration
}

// HostRegistration contains all host configuration returned by Register.
type HostRegistration struct {
	// Functions maps function names to their Go implementations.
	// These are registered with the WASM runtime as host imports.
	Functions map[string]any

	// YieldTypes are the yield types this host handles.
	// Used to convert asyncify suspensions to dispatcher commands.
	YieldTypes []YieldType
}

// YieldType describes a yield and how to handle it.
type YieldType struct {
	// CmdID is the dispatcher command ID for this yield type
	CmdID dispatcher.CommandID
}
