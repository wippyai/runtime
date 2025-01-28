package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/registry"
	lua "github.com/yuin/gopher-lua"
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

	// Callable represents an executable Lua function wrapper that can be invoked
	// with a context and arguments.
	Callable interface {
		// Execute runs the named Lua function with the given context and arguments,
		// returning the function's result or an error.
		Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error)
	}

	// Factory creates new instances of the Lua virtual machine with compiled code.
	// It handles the compilation and instantiation of Lua environments.
	Factory interface {
		// Compile prepares the Lua code for execution.
		Compile() error
		// MakeVM creates a new instance of the Lua virtual machine.
		MakeVM() (VM, error)
	}

	// FunctionProvider manages access to Lua function configurations in the system.
	// It allows looking up function configurations by their registry ID.
	FunctionProvider interface {
		// Get retrieves the function configuration for the given registry ID.
		Get(name registry.ID) (*FunctionConfig, error)
		// Has checks if a function configuration exists for the given registry ID.
		Has(name registry.ID) bool
	}

	// LibraryRegistry manages access to Lua library configurations in the system.
	// It allows looking up library configurations by their registry ID.
	LibraryRegistry interface {
		// Get retrieves the library configuration for the given registry ID.
		Get(name registry.ID) (*LibraryConfig, error)
		// Has checks if a library configuration exists for the given registry ID.
		Has(name registry.ID) bool
	}

	// ModuleRegistry manages the set of available Lua modules in the system.
	// It provides lookup capabilities for modules by their name.
	ModuleRegistry interface {
		// Get retrieves a module by its name.
		Get(name string) (Module, error)
		// Has checks if a module exists with the given name.
		Has(name string) bool
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
