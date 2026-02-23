// SPDX-License-Identifier: MPL-2.0

package ctx

import (
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

var moduleTable *lua.LTable

func init() {
	mod := lua.CreateTable(0, 2)
	mod.RawSetString("get", lua.LGoFunc(get))
	mod.RawSetString("all", lua.LGoFunc(all))
	mod.Immutable = true
	moduleTable = mod
}

// Module is the ctx module definition.
var Module = &luaapi.ModuleDef{
	Name:        "ctx",
	Description: "Context value read operations",
	Class:       []string{luaapi.ClassNondeterministic, luaapi.ClassWorkflow},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		return moduleTable, nil
	},
	Types: ModuleTypes,
}

// Error helpers

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func notFoundError(l *lua.LState, key string) int {
	err := lua.NewLuaError(l, "key not found: "+key).
		WithKind(lua.NotFound).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return internalError(l, "no context")
	}

	key := l.CheckString(1)
	if key == "" {
		return invalidError(l, "empty key")
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		return notFoundError(l, key)
	}

	val, ok := values.Get(key)
	if !ok {
		return notFoundError(l, key)
	}

	l.Push(toLuaValue(l, val))
	l.Push(lua.LNil)
	return 2
}

func all(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return internalError(l, "no context")
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.Push(l.CreateTable(0, 0))
		l.Push(lua.LNil)
		return 2
	}

	t := l.CreateTable(0, values.Len())
	values.Iterate(func(key string, val any) {
		t.RawSetString(key, toLuaValue(l, val))
	})

	l.Push(t)
	l.Push(lua.LNil)
	return 2
}

func toLuaValue(l *lua.LState, val any) lua.LValue {
	if val == nil {
		return lua.LNil
	}
	switch v := val.(type) {
	case string:
		return lua.LString(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case bool:
		return lua.LBool(v)
	case []byte:
		return lua.LString(v)
	case map[string]any:
		t := l.CreateTable(0, len(v))
		for k, val := range v {
			t.RawSetString(k, toLuaValue(l, val))
		}
		return t
	case []any:
		t := l.CreateTable(len(v), 0)
		for i, val := range v {
			t.RawSetInt(i+1, toLuaValue(l, val))
		}
		return t
	default:
		return lua.LNil
	}
}
