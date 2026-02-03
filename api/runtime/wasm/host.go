package wasm

import "github.com/wippyai/runtime/api/dispatcher"

// Host class constants for categorization (mirrors Lua module classes).
const (
	ClassDeterministic    = "deterministic"
	ClassNondeterministic = "nondeterministic"
	ClassIO               = "io"
	ClassNetwork          = "network"
	ClassTime             = "time"
	ClassProcess          = "process"
	ClassStorage          = "storage"
)

// HostInfo contains metadata about a WASM host module.
type HostInfo struct {
	Namespace   string   // WIT namespace (e.g., "wippy:clock")
	Description string   // Human-readable description
	Class       []string // Tags using Class* constants
}

// Host is the interface for WASM host modules.
type Host interface {
	// Info returns host metadata including namespace and classification.
	Info() HostInfo

	// Register returns the host registration with functions and yield types.
	Register() *HostRegistration
}

// HostRegistration contains host configuration returned by Register.
type HostRegistration struct {
	// Functions maps function names to their Go implementations.
	// Implementations should be api.GoModuleFunc or compatible signatures.
	Functions map[string]any

	// YieldTypes are the yield types this host handles.
	YieldTypes []YieldType
}

// YieldType describes a yield and its dispatcher command ID.
type YieldType struct {
	CmdID dispatcher.CommandID
}
