package eval

import (
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/yuin/gopher-lua"
)

// Program wraps an evalhost.Program for Lua.
type Program struct {
	program *evalhost.Program
}

var programMethods = map[string]lua.LGFunction{
	"method":  programMethod,
	"modules": programModules,
}

func checkProgram(l *lua.LState, idx int) *Program {
	ud := l.CheckUserData(idx)
	if v, ok := ud.Value.(*Program); ok {
		return v
	}
	l.ArgError(idx, "Program expected")
	return nil
}

// programMethod returns the method name.
func programMethod(l *lua.LState) int {
	prog := checkProgram(l, 1)
	if prog == nil {
		return 0
	}
	l.Push(lua.LString(prog.program.Method()))
	return 1
}

// programModules returns the allowed modules.
func programModules(l *lua.LState) int {
	prog := checkProgram(l, 1)
	if prog == nil {
		return 0
	}

	modules := prog.program.Modules()
	t := l.CreateTable(len(modules), 0)
	for i, m := range modules {
		t.RawSetInt(i+1, lua.LString(m))
	}
	l.Push(t)
	return 1
}

// GetProgram returns the underlying evalhost.Program.
func (p *Program) GetProgram() *evalhost.Program {
	return p.program
}
