package base64

import (
	"encoding/base64"
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module represents a base64 Lua module.
type Module struct {
	once        sync.Once
	moduleTable *lua.LTable
}

// NewBase64Module creates and returns a new instance of the base64 Module.
func NewBase64Module() *Module {
	return &Module{}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "base64",
		Description: "Base64 encoding and decoding",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 2)
		mod.RawSetString("encode", l.NewFunction(m.encode))
		mod.RawSetString("decode", l.NewFunction(m.decode))
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// encode implements base64 encoding of a given string.
func (*Module) encode(l *lua.LState) int {
	// Input validation errors - use ArgError
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	// Empty string is valid input
	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(str))
	l.Push(lua.LString(encoded))
	return 1
}

// decode implements base64 decoding.
func (*Module) decode(l *lua.LState) int {
	// Input validation errors - use ArgError
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	// Empty string is valid input
	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newBase64DecodeError(l, err))
		return 2
	}

	l.Push(lua.LString(decoded))
	return 1
}
