// Package lua provides Lua runtime integration.
package lua

import (
	"context"

	"github.com/wippyai/runtime/api/event"

	lua "github.com/yuin/gopher-lua"
)

const (
	System          event.System = "lua"
	InvalidateNodes event.Kind   = "lua.reset_code"
)

// Module class constants for consistent categorization
const (
	ClassDeterministic    = "deterministic"    // Same input = same output
	ClassNondeterministic = "nondeterministic" // Output varies (time, random)
	ClassIO               = "io"               // External I/O operations
	ClassNetwork          = "network"          // Network operations
	ClassEncoding         = "encoding"         // Data serialization
	ClassTime             = "time"             // Time-related
	ClassProcess          = "process"          // Process management
	ClassSecurity         = "security"         // Security operations
	ClassStorage          = "storage"          // Data storage
	ClassWorkflow         = "workflow"         // Workflow-safe replacements
)

// ModuleInfo contains metadata about a Lua module
type ModuleInfo struct {
	Name        string   // Module identifier (used for require)
	Description string   // Human-readable description
	Class       []string // Tags using Class* constants
}

type (
	// Factory creates new instances of the Lua virtual machine with compiled code.
	// It handles the compilation and instantiation of Lua environments.
	Factory interface {
		// Compile prepares the Lua code for execution.
		Compile() error
		// CreateVM creates a new instance of the Lua virtual machine.
		CreateVM() (VM, error)
	}

	// VM represents a Lua virtual machine instance that can execute Lua code.
	// It provides methods to run Lua functions and manage the VM lifecycle.
	VM interface {
		// Execute runs a Lua function with the given name and arguments in the VM.
		Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error)
		// Close cleans up the VM resources.
		Close()
	}
)
