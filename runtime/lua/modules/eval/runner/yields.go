// SPDX-License-Identifier: MPL-2.0

package runner

import (
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	payloadconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

// CompileYield is yielded by runner.compile to compile Lua source.
type CompileYield struct {
	Source       string
	Method       string
	Modules      []string
	Imports      map[string]registry.ID
	AllowClasses []string
}

var compileYieldPool = sync.Pool{
	New: func() any { return &CompileYield{} },
}

func AcquireCompileYield() *CompileYield {
	return compileYieldPool.Get().(*CompileYield)
}

func ReleaseCompileYield(y *CompileYield) {
	y.Source = ""
	y.Method = ""
	y.Modules = nil
	y.Imports = nil
	y.AllowClasses = nil
	compileYieldPool.Put(y)
}

func (y *CompileYield) Release()                    { ReleaseCompileYield(y) }
func (y *CompileYield) String() string              { return "<runner_compile_yield>" }
func (y *CompileYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *CompileYield) CmdID() dispatcher.CommandID { return evalhost.Compile }
func (y *CompileYield) ToCommand() dispatcher.Command {
	return evalhost.CompileCmd{
		Source:       y.Source,
		Method:       y.Method,
		Modules:      y.Modules,
		Imports:      y.Imports,
		AllowClasses: y.AllowClasses,
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
	Source        string
	Method        string
	Args          payload.Payloads
	Modules       []string
	Imports       map[string]registry.ID
	Context       map[string]any
	AllowClasses  []string
	CustomModules map[string]any
	AllowYields   []dispatcher.CommandID
}

var runYieldPool = sync.Pool{
	New: func() any { return &RunYield{} },
}

func AcquireRunYield() *RunYield {
	return runYieldPool.Get().(*RunYield)
}

func ReleaseRunYield(y *RunYield) {
	y.Source = ""
	y.Method = ""
	y.Args = nil
	y.Modules = nil
	y.Imports = nil
	y.Context = nil
	y.AllowClasses = nil
	y.CustomModules = nil
	y.AllowYields = nil
	runYieldPool.Put(y)
}

func (y *RunYield) Release()                    { ReleaseRunYield(y) }
func (y *RunYield) String() string              { return "<runner_run_yield>" }
func (y *RunYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *RunYield) CmdID() dispatcher.CommandID { return evalhost.Run }
func (y *RunYield) ToCommand() dispatcher.Command {
	return evalhost.RunCmd{
		Source:        y.Source,
		Method:        y.Method,
		Args:          y.Args,
		Modules:       y.Modules,
		Imports:       y.Imports,
		Context:       y.Context,
		AllowClasses:  y.AllowClasses,
		CustomModules: y.CustomModules,
		AllowYields:   y.AllowYields,
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

	lval, convErr := payloadconv.GoToLua(data)
	if convErr != nil {
		luaErr := lua.WrapErrorWithLua(l, convErr, "conversion failed").
			WithKind(lua.Internal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}
	return []lua.LValue{lval, lua.LNil}
}
