package engine

import (
	"fmt"
	"github.com/ponyruntime/go-lua"
	"go.uber.org/zap"
	"strings"
)

// WithLibrary adds a library from source code to the VM
func WithLibrary(name string, source string) Option {
	return func(vm *VM) {
		vm.log.Debug("loading library from source", zap.String("name", name))

		loader := func(L *lua.LState) int {
			fn, err := L.Load(strings.NewReader(source), fmt.Sprintf("<%s>", name))
			if err != nil {
				// Propagate the error by pushing it onto the stack
				L.Push(lua.LString(err.Error()))
				return 1 // Signal error
			}

			L.Push(fn)
			err = L.PCall(0, lua.MultRet, nil)
			if err != nil {
				// Propagate the error
				L.Push(lua.LString(err.Error()))
				return 1 // Signal error
			}

			if L.GetTop() > 0 && L.Get(-1).Type() != lua.LTTable {
				// Propagate the error: library did not return a table
				err := fmt.Errorf("library '%s' must return a table", name)
				L.Push(lua.LString(err.Error()))
				return 1 // Signal error
			}

			return 1 // Success
		}

		// Use vm.state.PreloadModule to register the loader
		vm.state.PreloadModule(name, loader)
	}
}

// WithLoader adds a library with a custom loader function to the VM
func WithLoader(name string, loader lua.LGFunction) Option {
	return func(vm *VM) {
		vm.log.Debug("loading library with loader", zap.String("name", name))
		vm.state.PreloadModule(name, loader)
	}
}

func WithGlobalFunction(name string, function lua.LGFunction) Option {
	return func(vm *VM) {
		vm.log.Debug("setting global function", zap.String("name", name))
		vm.state.SetGlobal(name, vm.state.NewFunction(function))
	}
}

func WithGlobalValue(name string, value lua.LValue) Option {
	return func(vm *VM) {
		vm.log.Debug("setting global value", zap.String("name", name))
		vm.state.SetGlobal(name, value)
	}
}
