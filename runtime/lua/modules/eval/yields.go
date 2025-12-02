package eval

import (
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

// CompileYield is yielded by eval.compile to compile Lua source.
type CompileYield struct {
	Source  string
	Method  string
	Modules []string
}

var compileYieldPool = sync.Pool{
	New: func() interface{} { return &CompileYield{} },
}

// AcquireCompileYield gets a CompileYield from pool.
func AcquireCompileYield() *CompileYield {
	return compileYieldPool.Get().(*CompileYield)
}

// ReleaseCompileYield returns a CompileYield to pool.
func ReleaseCompileYield(y *CompileYield) {
	y.Source = ""
	y.Method = ""
	y.Modules = nil
	compileYieldPool.Put(y)
}

func (y *CompileYield) Release()                    { ReleaseCompileYield(y) }
func (y *CompileYield) String() string              { return "<compile_yield>" }
func (y *CompileYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *CompileYield) CmdID() dispatcher.CommandID { return evalhost.CmdCompile }
func (y *CompileYield) ToCommand() dispatcher.Command {
	return evalhost.CompileCmd{
		Source:  y.Source,
		Method:  y.Method,
		Modules: y.Modules,
	}
}

// HandleResult converts the compile result to Lua Program userdata.
func (y *CompileYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	if data == nil {
		return []lua.LValue{lua.LNil, lua.LString("compilation returned nil")}
	}

	program, ok := data.(*evalhost.Program)
	if !ok {
		return []lua.LValue{lua.LNil, lua.LString("invalid program type")}
	}

	// Create Program userdata
	prog := &Program{program: program}
	ud := l.NewUserData()
	ud.Value = prog
	ud.Metatable = value.GetTypeMetatable(l, programTypeName)
	return []lua.LValue{ud}
}

// RunYield is yielded by eval.run to execute Lua code.
type RunYield struct {
	Source  string
	Method  string
	Args    []any
	Modules []string
	Context map[string]any
}

var runYieldPool = sync.Pool{
	New: func() interface{} { return &RunYield{} },
}

// AcquireRunYield gets a RunYield from pool.
func AcquireRunYield() *RunYield {
	return runYieldPool.Get().(*RunYield)
}

// ReleaseRunYield returns a RunYield to pool.
func ReleaseRunYield(y *RunYield) {
	y.Source = ""
	y.Method = ""
	y.Args = nil
	y.Modules = nil
	y.Context = nil
	runYieldPool.Put(y)
}

func (y *RunYield) Release()                    { ReleaseRunYield(y) }
func (y *RunYield) String() string              { return "<run_yield>" }
func (y *RunYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *RunYield) CmdID() dispatcher.CommandID { return evalhost.CmdRun }
func (y *RunYield) ToCommand() dispatcher.Command {
	return evalhost.RunCmd{
		Source:  y.Source,
		Method:  y.Method,
		Args:    y.Args,
		Modules: y.Modules,
		Context: y.Context,
	}
}

// HandleResult converts the run result to Lua value.
func (y *RunYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		return []lua.LValue{lua.LNil, lua.LString(err.Error())}
	}

	if data == nil {
		return []lua.LValue{lua.LNil}
	}

	// Convert Go value to Lua value
	return []lua.LValue{goToLua(l, data)}
}

// goToLua converts a Go value to Lua value.
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
