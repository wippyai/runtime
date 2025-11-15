package treesitter

import (
	lua "github.com/yuin/gopher-lua"
)

// pushError pushes a structured error table to the Lua stack.
// The error table contains:
//   - message: error message string
//   - __tostring: metamethod that returns the message
func pushError(l *lua.LState, err error) {
	if err == nil {
		l.Push(lua.LNil)
		return
	}

	errTable := l.CreateTable(0, 1)
	errTable.RawSetString("message", lua.LString(err.Error()))

	mt := l.CreateTable(0, 1)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		tbl := l.CheckTable(1)
		msg := tbl.RawGetString("message")
		l.Push(msg)
		return 1
	}))

	l.SetMetatable(errTable, mt)
	l.Push(errTable)
}

// pushErrorString pushes a structured error table from a string message to the Lua stack.
func pushErrorString(l *lua.LState, message string) {
	errTable := l.CreateTable(0, 1)
	errTable.RawSetString("message", lua.LString(message))

	mt := l.CreateTable(0, 1)
	mt.RawSetString("__tostring", l.NewFunction(func(l *lua.LState) int {
		tbl := l.CheckTable(1)
		msg := tbl.RawGetString("message")
		l.Push(msg)
		return 1
	}))

	l.SetMetatable(errTable, mt)
	l.Push(errTable)
}
