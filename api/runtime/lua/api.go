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

type (
	// Module represents a loadable Lua module that can be registered with the VM.
	// It provides methods to load the module into a Lua state and identify the module by name.
	Module interface {
		// Loader initializes the module in the given Lua state and returns the number of
		// values pushed onto the stack.
		Loader(*lua.LState) int
		// Name returns the identifier for this module.
		Name() string
	}

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
