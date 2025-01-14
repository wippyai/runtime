package engine

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
	"strings"

	"github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"go.uber.org/zap"
)

// Option represents a VM configuration option
type Option func(*VM)

// VM represents a Lua virtual machine instance
type VM struct {
	log        *zap.Logger
	state      *lua.LState
	funcs      map[string]lua.LValue
	initErrors []error
}

// NewVM creates a new VM instance with the provided script and options
func NewVM(log *zap.Logger, opts ...Option) (*VM, error) {
	state, err := newLuaState()
	if err != nil {
		return nil, fmt.Errorf("failed to create Lua State: %w", err)
	}

	vm := &VM{
		log:        log,
		state:      state,
		funcs:      make(map[string]lua.LValue),
		initErrors: []error{},
	}

	for _, opt := range opts {
		opt(vm)
	}

	if len(vm.initErrors) > 0 {
		return nil, fmt.Errorf("VM initialization errors: %s", strings.Join(collectErrors(vm.initErrors), "; "))
	}

	return vm, nil
}

func collectErrors(errors []error) []string {
	result := make([]string, len(errors))
	for i, err := range errors {
		result[i] = err.Error()
	}
	return result
}

// CompileFunction loads a script and stores its named function
func (v *VM) CompileFunction(name, script string) error {
	chunk, err := parse.Parse(strings.NewReader(script), name)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	// todo: use more effective to pre-compile bytecode outside of a single VM
	fnProto, err := lua.Compile(chunk, name)
	if err != nil {
		return fmt.Errorf("compile error: %w", err)
	}

	fn := v.state.NewFunctionFromProto(fnProto)
	v.state.Push(fn)

	// todo: wait what?
	if err := v.state.PCall(0, 1, nil); err != nil {
		return fmt.Errorf("execution error: %w", err)
	}

	//if err := v.storeFunctionResult(name); err != nil {
	//	return err
	//}

	return nil
}

func (v *VM) storeFunctionResult(name string) error {
	if v.state.GetTop() < 1 {
		return fmt.Errorf("no function result for %q", name)
	}

	// Try direct function return
	if v.state.Get(-1).Type() == lua.LTFunction {
		v.funcs[name] = v.state.Get(-1)
		v.state.Pop(1)
		return nil
	}

	// Try module table return
	if v.state.Get(-1).Type() == lua.LTTable {
		if fn := v.state.Get(-1).(*lua.LTable).RawGetString(name); fn.Type() == lua.LTFunction {
			v.funcs[name] = fn
			v.state.Pop(1)
			return nil
		}
	}

	// Try global function
	if fn := v.state.GetGlobal(name); fn.Type() == lua.LTFunction {
		v.funcs[name] = fn
		if v.state.GetTop() >= 1 {
			v.state.Pop(1)
		}
		return nil
	}

	return fmt.Errorf("function %q not found", name)
}

// Execute runs the named function with provided arguments
func (v *VM) Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error) {
	fn, ok := v.funcs[funcName]
	if !ok {
		return nil, fmt.Errorf("function %q not found", funcName)
	}

	if ctx != nil {
		ctx, cleanup := closer.WithContext(ctx)
		defer func() {
			v.state.RemoveContext()
			if err := cleanup.Close(); err != nil {
				v.log.Error("cleanup failed",
					zap.String("function", funcName),
					zap.Error(err))
			}
		}()
		v.state.SetContext(ctx)
	}

	return v.callFunction(fn, args)
}

func (v *VM) callFunction(fn lua.LValue, args []lua.LValue) (lua.LValue, error) {
	v.state.Push(fn)
	for _, arg := range args {
		v.state.Push(arg)
	}

	if err := v.state.PCall(len(args), 1, nil); err != nil {
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

// DoString executes a Lua string with given context and arguments
func (v *VM) DoString(ctx context.Context, s string, name string, args ...lua.LValue) error {
	fn, err := v.state.Load(strings.NewReader(s), fmt.Sprintf("<%s>", name))
	if err != nil {
		return fmt.Errorf("load error: %w", err)
	}

	if ctx != nil {
		ctx, cleanup := closer.WithContext(ctx)
		defer func() {
			v.state.RemoveContext()
			if err := cleanup.Close(); err != nil {
				v.log.Error("cleanup failed",
					zap.String("do_string", name),
					zap.Error(err))
			}
		}()
		v.state.SetContext(ctx)
	}

	v.state.Push(fn)
	for _, arg := range args {
		v.state.Push(arg)
	}

	return v.state.PCall(len(args), lua.MultRet, nil)
}

func (v *VM) State() *lua.LState {
	return v.state
}
