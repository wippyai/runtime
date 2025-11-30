package base64

import (
	"encoding/base64"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	lua "github.com/yuin/gopher-lua"
)

var (
	moduleTable  *lua.LTable
	registration *lua2api.Registration
	initOnce     sync.Once
)

// Module is the singleton base64 module instance.
var Module = &base64Module{}

type base64Module struct{}

func (m *base64Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "base64",
		Description: "Base64 encoding and decoding",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *base64Module) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		mod := &lua.LTable{}
		mod.RawSetString("encode", lua.LGoFunc(encode))
		mod.RawSetString("decode", lua.LGoFunc(decode))
		mod.Immutable = true
		moduleTable = mod

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})
	return registration
}

func (m *base64Module) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind binds the base64 module to the Lua state.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func encode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(str))
	l.Push(lua.LString(encoded))
	return 1
}

func decode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decoded))
	return 1
}
