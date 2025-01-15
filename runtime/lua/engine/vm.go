package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/internal/closer"
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
	exported   map[string]*lua.LFunction
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
		exported:   make(map[string]*lua.LFunction),
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

// Close closes the VM and releases resources
func (v *VM) Close() {
	if v.state != nil {
		v.state.Close()
	}
}

// Import loads a script and stores its named functions
func (v *VM) Import(s, name string, funcName ...string) error {
	if len(funcName) == 0 {
		return fmt.Errorf("no function names provided for export")
	}

	chunk, err := parse.Parse(strings.NewReader(s), name)
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	fnProto, err := lua.Compile(chunk, name)
	if err != nil {
		return fmt.Errorf("compile error: %w", err)
	}

	return v.Mount(fnProto, funcName...)
}

// Mount loads and mounts (executes) provided function(s) prototype
func (v *VM) Mount(proto *lua.FunctionProto, funcNames ...string) error {
	if len(funcNames) == 0 {
		return fmt.Errorf("no function names provided for mount")
	}

	if proto == nil {
		return fmt.Errorf("nil function prototype provided")
	}

	v.state.Push(v.state.NewFunctionFromProto(proto))

	if err := v.state.PCall(0, 1, nil); err != nil {
		return fmt.Errorf("execution error: %w", err)
	}

	if err := v.exportFunctions(funcNames...); err != nil {
		return fmt.Errorf("export error: %w", err)
	}

	return nil
}

// Execute runs the named function with provided arguments
func (v *VM) Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error) {
	fn, ok := v.exported[funcName]
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

// DoString executes a Lua string with given context and arguments.
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

func collectErrors(errors []error) []string {
	result := make([]string, len(errors))
	for i, err := range errors {
		result[i] = err.Error()
	}
	return result
}

func (v *VM) exportFunctions(funcNames ...string) error {
	if v.state.GetTop() < 1 {
		return fmt.Errorf("no functions available to export")
	}

	value := v.state.Get(-1)
	defer v.state.Pop(1)

	var exportErrors []string

	for _, funcName := range funcNames {
		var fn *lua.LFunction

		// Try direct function return
		if value.Type() == lua.LTFunction {
			fn = value.(*lua.LFunction)
		} else if value.Type() == lua.LTTable {
			// Try module table return
			if tableVal := value.(*lua.LTable).RawGetString(funcName); tableVal.Type() == lua.LTFunction {
				fn = tableVal.(*lua.LFunction)
			}
		}

		// Try global function if not found above
		if fn == nil {
			if global := v.state.GetGlobal(funcName); global.Type() == lua.LTFunction {
				fn = global.(*lua.LFunction)
			}
		}

		if fn != nil {
			v.exported[funcName] = fn
		} else {
			exportErrors = append(exportErrors, fmt.Sprintf("function %q not found", funcName))
		}
	}

	if len(exportErrors) > 0 {
		return fmt.Errorf("export errors: %s", strings.Join(exportErrors, "; "))
	}

	return nil
}

func (v *VM) callFunction(fn lua.LValue, args []lua.LValue) (lua.LValue, error) {
	v.state.Push(fn)
	for _, arg := range args {
		v.state.Push(arg)
	}

	if err := v.state.PCall(len(args), 1, nil); err != nil {
		return nil, fmt.Errorf("function call error: %w", err)
	}

	var result lua.LValue
	if v.state.GetTop() >= 1 {
		count := v.state.GetTop()
		for i := 1; i <= v.state.GetTop(); i++ {
			if i == 1 {
				result = v.state.Get(i)
			}
		}
		v.state.Pop(count)
	}

	return result, nil
}
