// Package workflow provides Lua yield support for deterministic workflow execution.
package workflow

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/workflow"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	lua "github.com/yuin/gopher-lua"
)

// Yield wraps a side effect closure for deterministic execution.
type Yield struct {
	Cmd *workflow.SideEffectCmd
}

var yieldPool = sync.Pool{
	New: func() any {
		return &Yield{Cmd: &workflow.SideEffectCmd{}}
	},
}

// NewYield gets a yield from the pool.
func NewYield(fn func() (any, error)) *Yield {
	y := yieldPool.Get().(*Yield)
	y.Cmd.Fn = fn
	return y
}

// Release returns the yield to the pool.
func (y *Yield) Release() {
	y.Cmd.Fn = nil
	yieldPool.Put(y)
}

func (y *Yield) String() string                { return "<workflow_side_effect>" }
func (y *Yield) Type() lua.LValueType          { return lua.LTUserData }
func (y *Yield) ToCommand() dispatcher.Command { return y.Cmd }
func (y *Yield) CmdID() dispatcher.CommandID   { return workflow.SideEffect }

// HandleResult converts the side effect result to Lua values.
func (y *Yield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "side effect failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}
	result, ok := data.(workflow.Result)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid side effect result type").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	if result.Error != nil {
		luaErr := lua.WrapErrorWithLua(l, result.Error, "")
		return []lua.LValue{lua.LNil, luaErr}
	}
	lv, convErr := luaconv.GoToLua(result.Value)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "result conversion failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lv, lua.LNil}
}
