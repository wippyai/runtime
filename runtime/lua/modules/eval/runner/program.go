package runner

import (
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	lua "github.com/wippyai/go-lua"
)

// Program wraps an evalhost.Program for Lua userdata.
type Program struct {
	program *evalhost.Program
}

var programMethods = map[string]lua.LGoFunc{
	"method":  programMethod,
	"modules": programModules,
}

func programMethod(l *lua.LState) int {
	prog := checkProgram(l, 1)
	if prog == nil {
		return 0
	}
	l.Push(lua.LString(prog.program.Method()))
	return 1
}

func programModules(l *lua.LState) int {
	prog := checkProgram(l, 1)
	if prog == nil {
		return 0
	}
	modules := prog.program.Modules()
	tbl := l.CreateTable(len(modules), 0)
	for i, m := range modules {
		tbl.RawSetInt(i+1, lua.LString(m))
	}
	l.Push(tbl)
	return 1
}

func checkProgram(l *lua.LState, idx int) *Program {
	ud := l.CheckUserData(idx)
	if prog, ok := ud.Value.(*Program); ok {
		return prog
	}
	l.ArgError(idx, "eval.runner.Program expected")
	return nil
}
