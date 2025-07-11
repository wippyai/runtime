package engine

import (
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// WithLibrary adds a library from either source code or function prototype to the VM.
// The source can be either a string containing Lua code or a *lua.FunctionProto.
// The library must return a table when loaded.
func WithLibrary(name string, source interface{}) Option {
	return func(vm *VM) {
		// Validate library name
		if name == "" {
			vm.initErrors = append(vm.initErrors, fmt.Errorf("library name cannot be empty"))
			return
		}

		// Check for invalid characters in library name
		invalidChars := "/\\. *"
		for _, char := range invalidChars {
			if strings.ContainsRune(name, char) {
				vm.initErrors = append(vm.initErrors, fmt.Errorf("library name contains invalid character: %c", char))
				return
			}
		}

		// Check name length
		if len(name) > 128 {
			vm.initErrors = append(vm.initErrors, fmt.Errorf("library name too long (max 128 characters)"))
			return
		}

		// Early validation of source
		switch s := source.(type) {
		case string:
			if s == "" {
				vm.initErrors = append(vm.initErrors, fmt.Errorf("library source cannot be empty"))
				return
			}
		case *lua.FunctionProto:
			if s == nil {
				vm.initErrors = append(vm.initErrors, fmt.Errorf("library prototype cannot be nil"))
				return
			}
		default:
			vm.initErrors = append(vm.initErrors, fmt.Errorf("invalid source type for library '%s': %T", name, source))
			return
		}

		// AddCleanup library to preload
		vm.state.PreloadModule(name, func(l *lua.LState) int {
			var fn *lua.LFunction

			switch s := source.(type) {
			case string:
				var err error
				fn, err = l.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
				if err != nil {
					vm.initErrors = append(vm.initErrors, fmt.Errorf("failed to load library '%s': %w", name, err))
					l.RaiseError("failed to load library: %v", err)
					return 0
				}
			case *lua.FunctionProto:
				fn = l.NewFunctionFromProto(s)
			}

			l.Push(fn)
			if err := l.PCall(0, lua.MultRet, nil); err != nil {
				vm.initErrors = append(vm.initErrors, fmt.Errorf("failed to initialize library '%s': %w", name, err))
				l.RaiseError("failed to initialize library: %v", err)
				return 0
			}

			if l.GetTop() == 0 {
				err := fmt.Errorf("library '%s' must return a value", name)
				vm.initErrors = append(vm.initErrors, err)
				l.RaiseError("%s", err.Error())
				return 0
			}

			if l.Get(-1).Type() != lua.LTTable {
				err := fmt.Errorf("library '%s' must return a table, got %s", name, l.Get(-1).Type().String())
				vm.initErrors = append(vm.initErrors, err)
				l.RaiseError("%s", err.Error())
				return 0
			}

			return 1
		})
	}
}

// todo: add with Module, see api

// WithLoader adds a library with a custom loader function to the VM.
// The loader function should return a table that will be used as the module.
func WithLoader(name string, loader lua.LGFunction) Option {
	return func(vm *VM) {
		vm.state.PreloadModule(name, loader)
	}
}

// WithPreloaded preloads a module using the provided loader function and sets
// the result as a global variable with the given name.
func WithPreloaded(name string, loader lua.LGFunction) Option {
	return func(vm *VM) {
		// todo: we can seriously optimize this function by adding immutable tables and sharing module tables
		// todo: using own vm section, it will drop the need to init modules at start of each process/workflow
		// todo: as well reduce overall memory usage. Implementation is not trivial, but possible.

		// Spawn module instance using loader
		L := vm.state
		L.Push(L.NewFunction(loader))
		err := L.PCall(0, lua.MultRet, nil)
		if err != nil {
			vm.initErrors = append(vm.initErrors, fmt.Errorf("preload %s failed: %w", name, err))
			return
		}

		// Set module output as global
		if L.GetTop() > 0 {
			L.SetGlobal(name, L.Get(-1))
			L.Pop(1)
		}
	}
}

// WithGlobalFunction adds a global function to the Lua state with the given name.
func WithGlobalFunction(name string, function lua.LGFunction) Option {
	return func(vm *VM) {
		vm.state.SetGlobal(name, vm.state.NewFunction(function))
	}
}

func WithFunctionProto(name string, proto *lua.FunctionProto) Option {
	return func(vm *VM) {
		err := vm.Mount(proto, name)
		if err != nil {
			vm.initErrors = append(vm.initErrors, fmt.Errorf("failed to load main function: %w", err))
		}
	}
}

// WithGlobalValue adds a global value to the Lua state with the given name.
func WithGlobalValue(name string, value lua.LValue) Option {
	return func(vm *VM) {
		vm.state.SetGlobal(name, value)
	}
}
