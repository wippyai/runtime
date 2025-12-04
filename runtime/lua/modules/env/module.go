package env

import (
	"github.com/wippyai/runtime/api/env"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

// Module is the env module definition.
var Module = &luaapi.ModuleDef{
	Name:        "env",
	Description: "Environment variable access",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := &lua.LTable{}
		mod.RawSetString("get", lua.LGoFunc(envGet))
		mod.RawSetString("set", lua.LGoFunc(envSet))
		mod.RawSetString("get_all", lua.LGoFunc(envGetAll))
		mod.Immutable = true
		return mod, nil
	},
}

func envGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	if !security.IsAllowed(ctx, "env.get", key, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to access environment variable: " + key))
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("environment registry not found"))
		return 2
	}

	value, err := envRegistry.Get(ctx, key)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(value))
	l.Push(lua.LNil)
	return 2
}

func envSet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	value := l.CheckString(2)

	if !security.IsAllowed(ctx, "env.set", key, nil) {
		l.Push(lua.LNil)
		l.Push(lua.LString("not allowed to set environment variable: " + key))
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("environment registry not found"))
		return 2
	}

	err := envRegistry.Set(ctx, key, value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func envGetAll(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("environment registry not found"))
		return 2
	}

	variables, err := envRegistry.All(ctx)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	result := l.CreateTable(0, len(variables))

	for key, value := range variables {
		if security.IsAllowed(ctx, "env.get", key, nil) {
			result.RawSetString(key, lua.LString(value))
		}
	}

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
