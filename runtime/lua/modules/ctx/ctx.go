package ctx

import (
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context" // Spawn sure this import path is correct
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	transcoder "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Module (ctx) gets or sets a context value found by a given key.
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
}

// NewCtxModule creates a new context module with the specified logger.
func NewCtxModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name.
func (m *Module) Name() string {
	return "ctx"
}

// Loader is the entry point for loading the module into Lua.
// It registers the get, set, and all functions into the Lua state.
func (m *Module) Loader(l *lua.LState) int {
	// Create module table once and cache it
	m.once.Do(func() {
		t := l.CreateTable(0, 3) // Now 3 functions: get, set, and all

		t.RawSetString("get", l.NewFunction(m.get))
		t.RawSetString("set", l.NewFunction(m.set))
		t.RawSetString("all", l.NewFunction(m.all))

		t.Immutable = true
		m.moduleTable = t
	})

	l.Push(m.moduleTable)
	return 1
}

// get retrieves a value from the context by key.
// Returns (value, nil) if found, (nil, error) if not found.
func (m *Module) get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	k := l.CheckString(1)
	if k == "" {
		l.ArgError(1, "empty key provided")
		return 0
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.ArgError(1, "invalid context")
		return 0
	}

	vv, ok := values.Get(k)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no value found for key: " + k))
		return 2
	}

	v, err := transcoder.GoToLua(vv)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("error converting value to Lua: " + err.Error()))
		return 2
	}

	l.Push(v)
	l.Push(lua.LNil)

	return 2
}

// set stores a value in the context with the specified key.
// Returns (true, nil) on success.
func (m *Module) set(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	k := l.CheckString(1)
	if k == "" {
		l.ArgError(1, "empty key provided")
		return 0
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.ArgError(1, "invalid context")
		return 0
	}

	values.Set(k, value.ToGoAny(l.CheckAny(2)))

	l.Push(lua.LTrue)
	l.Push(lua.LNil)

	return 2
}

// all retrieves all values from the context.
// Returns (table, nil) if successful, (nil, error) if an error occurs.
func (m *Module) all(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.ArgError(1, "no context found")
		return 0
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.ArgError(1, "invalid context")
		return 0
	}

	// Create a new table to hold all the values
	t := l.CreateTable(0, values.Len())

	// Iterate over all key-value pairs
	values.Iterate(func(key string, value any) {
		// Convert the Go value to a Lua value
		luaValue, err := transcoder.GoToLua(value)
		if err != nil {
			// Skip values that cannot be converted
			m.log.Warn("error converting value to Lua", zap.String("key", key), zap.Error(err))
			return
		}

		// Set the key-value pair in the table
		t.RawSetString(key, luaValue)
	})

	l.Push(t)
	l.Push(lua.LNil)

	return 2
}
