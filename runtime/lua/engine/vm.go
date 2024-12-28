package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/go-lua/parse"
	"go.uber.org/zap"
)

// VM represents a Lua virtual machine instance
type VM struct {
	log        *zap.Logger
	state      *lua.LState
	funcs      map[string]lua.LValue
	initErrors []error
}

// Option represents a VM configuration option
type Option func(*VM)

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

// NewVM creates a new VM instance with the provided script and options
func NewVM(log *zap.Logger, opts ...Option) (*VM, error) {
	state := lua.NewState(lua.Options{})
	vm := &VM{
		log:        log,
		state:      state,
		funcs:      make(map[string]lua.LValue),
		initErrors: []error{},
	}

	// Apply all options
	for _, opt := range opts {
		opt(vm)
	}

	// Check if any errors occurred during initialization
	if len(vm.initErrors) > 0 {
		// Concatenate all errors into a single error message
		var errMsg strings.Builder
		for _, err := range vm.initErrors {
			errMsg.WriteString(err.Error())
			errMsg.WriteString("; ")
		}
		return nil, fmt.Errorf("errors during VM initialization: %s", errMsg.String())
	}

	return vm, nil
}

// CompileFunction loads a script and stores its named function
func (v *VM) CompileFunction(name, script string) error {
	chunk, err := parse.Parse(strings.NewReader(script), name)
	if err != nil {
		return err
	}

	fnProto, err := lua.Compile(chunk, name)
	if err != nil {
		return err
	}

	fn := v.state.NewFunctionFromProto(fnProto)
	v.state.Push(fn)

	err = v.state.PCall(0, 1, nil)
	if err != nil {
		return err
	}

	if v.state.GetTop() >= 1 {
		v.funcs[name] = v.state.Get(-1)
		v.state.Pop(1)
	}

	return nil
}

func (v *VM) DoString(s, name string) error {
	fn, err := v.state.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
	if err != nil {
		return err
	}

	v.state.Push(fn)
	return v.state.PCall(0, lua.MultRet, nil)
}

// Execute runs the named function with provided arguments and returns Lua value
func (v *VM) Execute(ctx context.Context, name string, args lua.LValue) (lua.LValue, error) {
	fn, ok := v.funcs[name]
	if !ok {
		return nil, fmt.Errorf("function %q not found", name)
	}

	v.state.SetContext(ctx)
	defer v.state.SetContext(nil)

	v.state.Push(fn)
	v.state.Push(args)

	err := v.state.PCall(1, 1, nil)
	if err != nil {
		return nil, err
	}

	var result lua.LValue
	if v.state.GetTop() >= 1 {
		result = v.state.Get(-1)
		v.state.Pop(1)
	}

	return result, nil
}

func (v *VM) Close() {
	if v.state != nil {
		v.state.Close()
	}
}

func (v *VM) State() *lua.LState {
	return v.state
}
