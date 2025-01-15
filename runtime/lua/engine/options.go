package engine

import (
	"fmt"
	"github.com/yuin/gopher-lua"
	"strings"
)

// WithLibrary adds a library from either source code or function prototype to the VM
func WithLibrary(name string, source interface{}) Option {
	return func(vm *VM) {
		loader := func(L *lua.LState) int {
			var fn *lua.LFunction

			switch s := source.(type) {
			case string:
				// Source code path
				var err error
				fn, err = L.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
				if err != nil {
					L.Push(lua.LString(err.Error()))
					return 1
				}
			case *lua.FunctionProto:
				// Function prototype path
				fn = L.NewFunctionFromProto(s)
			default:
				L.Push(lua.LString(fmt.Sprintf("invalid source type for library '%s'", name)))
				return 1
			}

			L.Push(fn)
			err := L.PCall(0, lua.MultRet, nil)
			if err != nil {
				L.Push(lua.LString(err.Error()))
				return 1
			}

			if L.GetTop() > 0 && L.Get(-1).Type() != lua.LTTable {
				err := fmt.Errorf("library '%s' must return a table", name)
				L.Push(lua.LString(err.Error()))
				return 1
			}

			return 1
		}

		vm.state.PreloadModule(name, loader)
	}
}

// WithLoader adds a library with a custom loader function to the VM
func WithLoader(name string, loader lua.LGFunction) Option {
	return func(vm *VM) {
		vm.state.PreloadModule(name, loader)
	}
}

func WithPreloaded(name string, loader lua.LGFunction) Option {
	return func(vm *VM) {
		// Create module instance using loader
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

func WithGlobalFunction(name string, function lua.LGFunction) Option {
	return func(vm *VM) {
		vm.state.SetGlobal(name, vm.state.NewFunction(function))
	}
}

func WithGlobalValue(name string, value lua.LValue) Option {
	return func(vm *VM) {
		vm.state.SetGlobal(name, value)
	}
}
