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

func (v *VM) DoString(ctx context.Context, s, name string) error {
	fn, err := v.state.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
	if err != nil {
		return err
	}

	v.state.Push(fn)

	if ctx != nil {
		v.state.SetContext(ctx)
		defer v.state.RemoveContext()
	}

	return v.state.PCall(0, lua.MultRet, nil)
}

// Execute runs the named function with provided arguments and returns Lua value
func (v *VM) Execute(ctx context.Context, name string, args lua.LValue) (lua.LValue, error) {
	fn, ok := v.funcs[name]
	if !ok {
		return nil, fmt.Errorf("function %q not found", name)
	}

	if ctx != nil {
		v.state.SetContext(ctx)
		defer v.state.RemoveContext()
	}

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
