package env

import (
	"sync"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

// Module provides Lua bindings for accessing environment variables
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// NewEnvModule creates a new environment module
func NewEnvModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "env"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *lua.LState) {
	t := l.CreateTable(0, 2) // Exactly 2 functions: get and get_all

	t.RawSetString("get", l.NewFunction(m.get))
	t.RawSetString("get_all", l.NewFunction(m.getAll))
	t.RawSetString("set", l.NewFunction(m.set))

	// Make the table immutable so it can be safely reused
	t.Immutable = true

	m.moduleTable = t
}

func (m *Module) get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}

	// todo: fix duplicate
	if _, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string]); !ok {
		l.RaiseError("invalid environment context")
		return 0
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	// Add security check for accessing specific environment variable
	if !security.IsAllowed(l.Context(), "env.get", key, nil) {
		l.RaiseError("not allowed to access environment variable: %s", key)
		return 0
	}

	envRegistry := env.GetRegistry(l.Context())
	if envRegistry == nil {
		l.RaiseError("environment registry not found")
		return 0
	}

	value, err := envRegistry.Get(l.Context(), key)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(value))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) set(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}

	if _, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string]); !ok {
		l.RaiseError("invalid environment context")
		return 0
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	value := l.CheckString(2)
	if value == "" {
		l.ArgError(2, "empty value")
		return 0
	}

	// Add security check for setting specific environment variable
	if !security.IsAllowed(l.Context(), "env.set", key, nil) {
		l.RaiseError("not allowed to set environment variable: %s", key)
		return 0
	}

	envRegistry := env.GetRegistry(l.Context())
	if envRegistry == nil {
		l.RaiseError("environment registry not found")
		return 0
	}

	err := envRegistry.Set(l.Context(), key, value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func (m *Module) getAll(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}

	envCtx, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string])
	if !ok {
		l.RaiseError("invalid environment context")
		return 0
	}

	envRegistry := env.GetRegistry(l.Context())
	if envRegistry == nil {
		l.RaiseError("environment registry not found")
		return 0
	}

	result := l.CreateTable(0, envCtx.Len())

	// Optimize by working with table internals directly
	if result.Strdict == nil {
		result.Strdict = make(map[string]lua.LValue, envCtx.Len())
	}

	envCtx.Iterate(func(key string, value string) {
		// Only include variables that the user has permission to access
		if security.IsAllowed(l.Context(), "env.get", key, nil) {
			// Direct map access instead of RawSetString for performance
			result.Strdict[key] = lua.LString(value)
		}
	})

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
