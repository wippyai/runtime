package engine

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
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

	// Case 1: Function was returned directly
	if v.state.GetTop() >= 1 && v.state.Get(-1).Type() == lua.LTFunction {
		v.funcs[name] = v.state.Get(-1)
		v.state.Pop(1)
		return nil
	}

	// Case 2: Module table was returned
	if v.state.GetTop() >= 1 && v.state.Get(-1).Type() == lua.LTTable {
		if fn := v.state.Get(-1).(*lua.LTable).RawGetString(name); fn.Type() == lua.LTFunction {
			v.funcs[name] = fn
			v.state.Pop(1)
			return nil
		}
	}

	// Case 3: Function was declared globally
	if fn := v.state.GetGlobal(name); fn.Type() == lua.LTFunction {
		v.funcs[name] = fn
		if v.state.GetTop() >= 1 {
			v.state.Pop(1) // Clean up stack if we had a return value
		}
		return nil
	}

	return fmt.Errorf("function %q not found", name)
}

func (v *VM) DoString(ctx context.Context, s, name string, args ...lua.LValue) error {
	fn, err := v.state.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
	if err != nil {
		return err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Attach cleanup to context
	ctx, cleanup := closer.WithContext(ctx)
	defer func() {
		if err := cleanup.Close(); err != nil {
			v.log.Error("failed to cleanup resources",
				zap.String("do_string", name),
				zap.Error(err))
		}
	}()

	if ctx != nil {
		v.state.SetContext(ctx)
		defer v.state.RemoveContext()
	}

	v.state.Push(fn)

	// Push all provided arguments onto the stack
	for _, arg := range args {
		v.state.Push(arg)
	}

	return v.state.PCall(len(args), lua.MultRet, nil)
}

// Execute runs the named function with provided arguments and returns Lua value
func (v *VM) Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error) {
	fn, ok := v.funcs[funcName]
	if !ok {
		return nil, fmt.Errorf("function %q not found", funcName)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Attach cleanup to context
	ctx, cleanup := closer.WithContext(ctx)
	defer func() {
		if err := cleanup.Close(); err != nil {
			v.log.Error("failed to cleanup resources",
				zap.String("function", funcName),
				zap.Error(err))
		}
	}()

	if ctx != nil {
		v.state.SetContext(ctx)
		defer v.state.RemoveContext()
	}

	v.state.Push(fn)
	for _, arg := range args {
		v.state.Push(arg)
	}

	err := v.state.PCall(len(args), 1, nil)
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
