package env

import (
	"sync"

	"github.com/wippyai/runtime/api/env"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton env module instance.
var Module = &envModule{}

type envModule struct{}

func (m *envModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "env",
		Description: "Environment variable access",
		Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}
}

func (m *envModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()
		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *envModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 3)

	mod.RawSetString("get", lua.LGoFunc(get))
	mod.RawSetString("set", lua.LGoFunc(set))
	mod.RawSetString("get_all", lua.LGoFunc(getAll))
	mod.Immutable = true

	return mod
}

func get(l *lua.LState) int {
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

func set(l *lua.LState) int {
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

func getAll(l *lua.LState) int {
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
