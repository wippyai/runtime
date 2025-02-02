package env

import (
	ctxapi "github.com/ponyruntime/pony/api/context"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module provides Lua bindings for accessing environment variables
type Module struct {
	log *zap.Logger
}

// New creates a new environment module
func New(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "env"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()
	l.SetFuncs(t, map[string]lua.LGFunction{
		"get":     m.get,
		"get_all": m.getAll,
	})
	l.Push(t)
	return 1
}

func (m *Module) get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context found")
		return 0
	}

	key := l.CheckString(1)
	if key == "" {
		l.ArgError(1, "empty key")
		return 0
	}

	envCtx, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string])
	if !ok {
		l.RaiseError("invalid environment context")
		return 0
	}

	value, ok := envCtx.Value(key)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	l.Push(lua.LString(value))
	l.Push(lua.LNil)
	return 2
}

func (m *Module) getAll(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	envCtx, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string])
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid environment context"))
		return 2
	}

	result := l.NewTable()

	envCtx.Iterate(func(key string, value string) {
		result.RawSetString(key, lua.LString(value))
	})

	l.Push(result)
	l.Push(lua.LNil)
	return 2
}
