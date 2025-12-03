package ctx

import (
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton ctx module instance.
var Module = &ctxModule{}

type ctxModule struct{}

func (m *ctxModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "ctx",
		Description: "Context value get/set operations",
		Class:       []string{luaapi.ClassNondeterministic},
	}
}

func (m *ctxModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := lua.CreateTable(0, 3)
		mod.RawSetString("get", lua.LGoFunc(get))
		mod.RawSetString("set", lua.LGoFunc(set))
		mod.RawSetString("all", lua.LGoFunc(all))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *ctxModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func get(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty key"))
		return 2
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no values in context"))
		return 2
	}

	val, ok := values.Get(key)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("key not found: " + key))
		return 2
	}

	l.Push(toLuaValue(l, val))
	l.Push(lua.LNil)
	return 2
}

func set(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("no context"))
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		l.Push(lua.LFalse)
		l.Push(lua.LString("empty key"))
		return 2
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.Push(lua.LFalse)
		l.Push(lua.LString("no values in context"))
		return 2
	}

	val := toGoValue(l.CheckAny(2))
	values.Set(key, val)

	l.Push(lua.LTrue)
	l.Push(lua.LNil)
	return 2
}

func all(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	values := ctxapi.GetValues(ctx)
	if values == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no values in context"))
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
	default:
		return lua.LNil
	}
}

func toGoValue(lv lua.LValue) any {
	switch v := lv.(type) {
	case lua.LString:
		return string(v)
	case lua.LNumber:
		return float64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return nil
	default:
		return nil
	}
}
