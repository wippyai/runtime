package json

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton json module instance.
var Module = &jsonModule{}

type jsonModule struct{}

func (m *jsonModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "json",
		Description: "JSON encoding and decoding",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *jsonModule) Register(l *lua.LState) *luaapi.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *jsonModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	luaapi.LoadModule(l, Module)
}

func encodeFunc(l *lua.LState) int {
	value := l.Get(1)
	if value == lua.LNil {
		l.Push(lua.LString("null"))
		return 1
	}

	data, err := Encode(value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(data))
	return 1
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	if str == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty string is not valid JSON"))
		return 2
	}

	value, err := Decode([]byte(str))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(value)
	return 1
}
