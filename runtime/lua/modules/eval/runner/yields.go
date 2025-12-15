package runner

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

// CompileYield is yielded by runner.compile to compile Lua source.
type CompileYield struct {
	Source  string
	Method  string
	Modules []string
}

var compileYieldPool = sync.Pool{
	New: func() interface{} { return &CompileYield{} },
}

func AcquireCompileYield() *CompileYield {
	return compileYieldPool.Get().(*CompileYield)
}

func ReleaseCompileYield(y *CompileYield) {
	y.Source = ""
	y.Method = ""
	y.Modules = nil
	compileYieldPool.Put(y)
}

func (y *CompileYield) Release()                    { ReleaseCompileYield(y) }
func (y *CompileYield) String() string              { return "<runner_compile_yield>" }
func (y *CompileYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *CompileYield) CmdID() dispatcher.CommandID { return evalhost.Compile }
func (y *CompileYield) ToCommand() dispatcher.Command {
	return evalhost.CompileCmd{
		Source:  y.Source,
		Method:  y.Method,
		Modules: y.Modules,
	}
}

func (y *CompileYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "compile failed").
			WithKind(lua.Internal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if data == nil {
		luaErr := lua.NewLuaError(l, "compilation returned nil").
			WithKind(lua.Internal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	program, ok := data.(*evalhost.Program)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid program type").
			WithKind(lua.Internal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	prog := &Program{program: program}
	ud := value.NewTypedUserData(l, prog, programTypeName)
	return []lua.LValue{ud, lua.LNil}
}

// RunYield is yielded by runner.run to execute Lua code.
type RunYield struct {
	Source  string
	Method  string
	Args    payload.Payloads
	Modules []string
	Context map[string]any
}

var runYieldPool = sync.Pool{
	New: func() interface{} { return &RunYield{} },
}

func AcquireRunYield() *RunYield {
	return runYieldPool.Get().(*RunYield)
}

func ReleaseRunYield(y *RunYield) {
	y.Source = ""
	y.Method = ""
	y.Args = nil
	y.Modules = nil
	y.Context = nil
	runYieldPool.Put(y)
}

func (y *RunYield) Release()                    { ReleaseRunYield(y) }
func (y *RunYield) String() string              { return "<runner_run_yield>" }
func (y *RunYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *RunYield) CmdID() dispatcher.CommandID { return evalhost.Run }
func (y *RunYield) ToCommand() dispatcher.Command {
	return evalhost.RunCmd{
		Source:  y.Source,
		Method:  y.Method,
		Args:    y.Args,
		Modules: y.Modules,
		Context: y.Context,
	}
}

func (y *RunYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "run failed").
			WithKind(lua.Internal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if data == nil {
		return []lua.LValue{lua.LNil, lua.LNil}
	}

	return []lua.LValue{goToLua(l, data), lua.LNil}
}

func goToLua(l *lua.LState, v any) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch val := v.(type) {
	case lua.LValue:
		return val
	case bool:
		return lua.LBool(val)
	case int:
		return lua.LNumber(val)
	case int64:
		return lua.LNumber(val)
	case float64:
		return lua.LNumber(val)
	case string:
		return lua.LString(val)
	case []byte:
		return lua.LString(val)
	case error:
		return lua.LString(val.Error())
	case []any:
		tbl := l.CreateTable(len(val), 0)
		for i, item := range val {
			tbl.RawSetInt(i+1, goToLua(l, item))
		}
		return tbl
	case map[string]any:
		tbl := l.CreateTable(0, len(val))
		for k, item := range val {
			tbl.RawSetString(k, goToLua(l, item))
		}
		return tbl
	default:
		return lua.LString(fmt.Sprintf("%v", val))
	}
}
