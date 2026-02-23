// SPDX-License-Identifier: MPL-2.0

package env

import (
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/env"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/security"
)

// Module is the env module definition.
var Module = &luaapi.ModuleDef{
	Name:        "env",
	Description: "Environment variable access",
	Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 3)
		mod.RawSetString("get", lua.LGoFunc(envGet))
		mod.RawSetString("set", lua.LGoFunc(envSet))
		mod.RawSetString("get_all", lua.LGoFunc(envGetAll))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func envGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		luaErr := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		luaErr := lua.NewLuaError(l, "empty key").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if !security.IsAllowed(ctx, "env.get", key, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to access environment variable: "+key).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		luaErr := lua.NewLuaError(l, "environment registry not found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	value, err := envRegistry.Get(ctx, key)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "get environment variable failed")
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}
	l.Push(lua.LString(value))
	l.Push(lua.LNil)
	return 2
}

func envSet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		luaErr := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		luaErr := lua.NewLuaError(l, "empty key").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	value := l.CheckString(2)

	if !security.IsAllowed(ctx, "env.set", key, nil) {
		luaErr := lua.NewLuaError(l, "not allowed to set environment variable: "+key).
			WithKind(lua.PermissionDenied).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		luaErr := lua.NewLuaError(l, "environment registry not found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	err := envRegistry.Set(ctx, key, value)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "set environment variable failed")
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func envGetAll(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		luaErr := lua.NewLuaError(l, "no context found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	envRegistry := env.GetRegistry(ctx)
	if envRegistry == nil {
		luaErr := lua.NewLuaError(l, "environment registry not found").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	variables, err := envRegistry.All(ctx)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "get all environment variables failed")
		l.Push(lua.LNil)
		l.Push(luaErr)
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
