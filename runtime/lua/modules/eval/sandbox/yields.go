package sandbox

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

// CompileYield is yielded by sandbox.compile to compile Lua source.
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
func (y *CompileYield) String() string              { return "<sandbox_compile_yield>" }
func (y *CompileYield) Type() lua.LValueType        { return lua.LTUserData }
func (y *CompileYield) CmdID() dispatcher.CommandID { return evalhost.CmdCompile }
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
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if data == nil {
		luaErr := lua.NewLuaError(l, "compilation returned nil").
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	program, ok := data.(*evalhost.Program)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid program type").
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	wrapper := &ProgramWrapper{program: program}
	ud := value.NewTypedUserData(l, wrapper, programTypeName)
	return []lua.LValue{ud, lua.LNil}
}

// ProgramWrapper wraps evalhost.Program for Lua userdata.
type ProgramWrapper struct {
	program *evalhost.Program
}

func (w *ProgramWrapper) Program() *evalhost.Program {
	return w.program
}

// CreateProcessYield is yielded to create a steppable process from a Program.
type CreateProcessYield struct {
	Program *evalhost.Program
	Clock   *MockClock
	Ctx     context.Context
}

var createProcessYieldPool = sync.Pool{
	New: func() interface{} { return &CreateProcessYield{} },
}

func AcquireCreateProcessYield() *CreateProcessYield {
	return createProcessYieldPool.Get().(*CreateProcessYield)
}

func ReleaseCreateProcessYield(y *CreateProcessYield) {
	y.Program = nil
	y.Clock = nil
	y.Ctx = nil
	createProcessYieldPool.Put(y)
}

func (y *CreateProcessYield) Release()             { ReleaseCreateProcessYield(y) }
func (y *CreateProcessYield) String() string       { return "<sandbox_create_process_yield>" }
func (y *CreateProcessYield) Type() lua.LValueType { return lua.LTUserData }

func (y *CreateProcessYield) CmdID() dispatcher.CommandID { return evalhost.CmdCreateProcess }

func (y *CreateProcessYield) ToCommand() dispatcher.Command {
	return evalhost.CreateProcessCmd{
		Program: y.Program,
	}
}

func (y *CreateProcessYield) HandleResult(l *lua.LState, data any, err error) []lua.LValue {
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "create process failed").
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	if data == nil {
		luaErr := lua.NewLuaError(l, "create process returned nil").
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	proc, ok := data.(process.Process)
	if !ok {
		luaErr := lua.NewLuaError(l, "invalid process type").
			WithKind(lua.KindInternal).WithRetryable(false)
		return []lua.LValue{lua.LNil, luaErr}
	}

	// Wrap in SandboxProcess for Lua control
	wrapper := NewSandboxProcess(y.Ctx, proc, y.Clock)
	ud := value.NewTypedUserData(l, wrapper, processTypeName)
	return []lua.LValue{ud, lua.LNil}
}
