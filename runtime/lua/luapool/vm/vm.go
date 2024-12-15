package vm

import (
	"context"
	"github.com/ponyruntime/pony/api/runtime"
	"strings"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/go-lua/parse"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
)

type VM struct {
	log   *zap.Logger
	state *lua.LState
	fn    lua.LValue
}

// TODO options with modules
func New(log *zap.Logger, script, main string, modules ...runtime.LuaModule) (*VM, error) {
	state := lua.NewState(lua.Options{})

	for _, module := range modules {
		log.Debug("preloading module", zap.String("module", module.Name()))
		state.PreloadModule(module.Name(), module.Loader)
	}

	// parse and compile into the lua.FunctionProto
	// parse and compile should be done only once
	chunk, err := parse.Parse(strings.NewReader(script), main)
	if err != nil {
		return nil, err
	}

	fnProto, err := lua.Compile(chunk, main)
	if err != nil {
		return nil, err
	}
	// ----------------------------- END parse and compile

	// initialize the function
	fn := state.NewFunctionFromProto(fnProto)
	state.Push(fn)

	// init
	err = state.PCall(0, 1, nil)
	if err != nil {
		return nil, err
	}

	// precompile modules
	// save moduleName -> functions names

	return &VM{
		log:   log,
		state: state,
		fn:    state.GetGlobal(main),
	}, nil
}

func (v *VM) Execute(ctx context.Context, args any) (string, error) {
	v.log.Debug("executing on VM", zap.Any("args", args))
	v.state.SetContext(ctx)
	v.state.Push(v.fn)

	// push args ---
	lv := engine.GoToLua(v.state, args)
	v.state.Push(lv)
	// ---- args ---

	// set args
	err := v.state.PCall(1, 1, nil)
	if err != nil {
		return "", err
	}

	var result lua.LValue
	if v.state.GetTop() >= 1 {
		result = v.state.Get(-1)
		v.state.Pop(1)
	}

	if result.Type() == lua.LTNil {
		return "", nil
	}

	return result.String(), nil
}
