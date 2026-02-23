// SPDX-License-Identifier: MPL-2.0

// Package io provides terminal IO operations for Lua scripts.
package io

import (
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/service/terminal"
)

// Module is the io module definition.
var Module = &luaapi.ModuleDef{
	Name:        "io",
	Description: "Terminal IO operations (stdin, stdout, stderr)",
	Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
	Build:       buildModule,
	Types:       ModuleTypes,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 8)

	mod.RawSetString("write", lua.LGoFunc(ioWrite))
	mod.RawSetString("print", lua.LGoFunc(ioPrint))
	mod.RawSetString("eprint", lua.LGoFunc(ioEprint))
	mod.RawSetString("read", lua.LGoFunc(ioReadYielding))
	mod.RawSetString("readline", lua.LGoFunc(ioReadlineYielding))
	mod.RawSetString("raw", lua.LGoFunc(ioRaw))
	mod.RawSetString("flush", lua.LGoFunc(ioFlush))
	mod.RawSetString("args", lua.LGoFunc(ioArgs))

	mod.Immutable = true
	return mod, yieldTypes
}

// ioWrite writes strings to stdout without newline.
// io.write(str1, str2, ...)
func ioWrite(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).
			WithRetryable(false))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		s := l.ToString(i)
		_, err := tc.Stdout.Write([]byte(s))
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "write stdout").
				WithKind(lua.Internal))
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
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).
			WithRetryable(false))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		if i > 1 {
			_, _ = tc.Stdout.Write([]byte("\t"))
		}
		s := l.ToString(i)
		_, _ = tc.Stdout.Write([]byte(s))
	}
	_, _ = tc.Stdout.Write([]byte("\n"))

	l.Push(lua.LTrue)
	return 1
}

// ioEprint writes strings to stderr with newline.
// io.eprint(val1, val2, ...)
func ioEprint(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stderr == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).
			WithRetryable(false))
		return 2
	}

	n := l.GetTop()
	for i := 1; i <= n; i++ {
		if i > 1 {
			_, _ = tc.Stderr.Write([]byte("\t"))
		}
		s := l.ToString(i)
		_, _ = tc.Stderr.Write([]byte(s))
	}
	_, _ = tc.Stderr.Write([]byte("\n"))

	l.Push(lua.LTrue)
	return 1
}

// Flush flushes stdout if it supports flushing.
// io.flush() -> bool, err
func ioFlush(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil || tc.Stdout == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "no terminal context").
			WithKind(lua.Unavailable).
			WithRetryable(false))
		return 2
	}

	// Check if stdout supports Sync (like os.File)
	if syncer, ok := tc.Stdout.(interface{ Sync() error }); ok {
		if err := syncer.Sync(); err != nil {
			l.Push(lua.LNil)
			l.Push(lua.WrapErrorWithLua(l, err, "flush stdout").
				WithKind(lua.Internal))
			return 2
		}
	}

	l.Push(lua.LTrue)
	return 1
}

// ioArgs returns command line arguments as a Lua table.
// io.args() -> table
func ioArgs(l *lua.LState) int {
	tc := terminal.GetTerminalContext(l.Context())
	if tc == nil {
		l.Push(lua.CreateTable(0, 0))
		return 1
	}

	t := lua.CreateTable(len(tc.Args), 0)
	for i, arg := range tc.Args {
		t.RawSetInt(i+1, lua.LString(arg))
	}

	l.Push(t)
	return 1
}
