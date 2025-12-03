// Package io provides terminal IO operations for Lua scripts.
package io

import (
	"bufio"
	"fmt"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/service/terminal"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton io module instance.
var Module = &ioModule{}

type ioModule struct{}

func (m *ioModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "io",
		Description: "Terminal IO operations (stdin, stdout, stderr)",
		Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	}
}

func (m *ioModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

func (m *ioModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 8)

	mod.RawSetString("write", lua.LGoFunc(ioWrite))
	mod.RawSetString("print", lua.LGoFunc(ioPrint))
	mod.RawSetString("eprint", lua.LGoFunc(ioEprint))
	mod.RawSetString("read", lua.LGoFunc(ioRead))
	mod.RawSetString("readline", lua.LGoFunc(ioReadline))

	mod.Immutable = true
	return mod
}

// ioWrite writes strings to stdout without newline.
// io.write(str1, str2, ...)
func ioWrite(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		s := l.ToString(i)
		_, err := tc.Stdout.Write([]byte(s))
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	l.Push(lua.LTrue)
	return 1
}

// ioPrint writes strings to stdout with spaces between and newline at end.
// io.print(val1, val2, ...)
func ioPrint(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		if i > 1 {
			tc.Stdout.Write([]byte("\t"))
		}
		s := l.ToString(i)
		tc.Stdout.Write([]byte(s))
	}
	tc.Stdout.Write([]byte("\n"))

	l.Push(lua.LTrue)
	return 1
}

// ioEprint writes strings to stderr with newline.
// io.eprint(val1, val2, ...)
func ioEprint(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stderr == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		if i > 1 {
			tc.Stderr.Write([]byte("\t"))
		}
		s := l.ToString(i)
		tc.Stderr.Write([]byte(s))
	}
	tc.Stderr.Write([]byte("\n"))

	l.Push(lua.LTrue)
	return 1
}

// ioRead reads n bytes from stdin.
// io.read(n) -> string, err
func ioRead(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdin == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	n := l.OptInt(1, 1024)
	if n <= 0 {
		n = 1024
	}

	buf := make([]byte, n)
	read, err := tc.Stdin.Read(buf)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(buf[:read]))
	return 1
}

// ioReadline reads a line from stdin (up to newline).
// io.readline() -> string, err
func ioReadline(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdin == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	reader := bufio.NewReader(tc.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		// Return partial line if we got EOF with data
		if len(line) > 0 {
			l.Push(lua.LString(line))
			return 1
		}
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Strip trailing newline
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	l.Push(lua.LString(line))
	return 1
}

// Flush flushes stdout if it supports flushing.
// io.flush() -> bool, err
func ioFlush(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	// Check if stdout supports Sync (like os.File)
	if syncer, ok := tc.Stdout.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(err.Error()))
			return 2
		}
	}

	l.Push(lua.LTrue)
	return 1
}

// Printf writes formatted string to stdout.
// io.printf(format, ...) -> bool, err
func ioPrintf(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no terminal context"))
		return 2
	}

	format := l.CheckString(1)
	args := make([]interface{}, l.GetTop()-1)
	for i := 2; i <= l.GetTop(); i++ {
		args[i-2] = luaValueToGo(l.Get(i))
	}

	_, err := fmt.Fprintf(tc.Stdout, format, args...)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func luaValueToGo(v lua.LValue) interface{} {
	switch v.Type() {
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return bool(v.(lua.LBool))
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTString:
		return string(v.(lua.LString))
	default:
		return v.String()
	}
}
