package base64

import (
	"encoding/base64"
	"github.com/ponyruntime/pony/runtime/lua/engine"

	lua "github.com/yuin/gopher-lua"
)

// Module represents a base64 Lua module.
type Module struct{}

// NewBase64Module creates and returns a new instance of the base64 Module.
func NewBase64Module() *Module {
	return &Module{}
}

// Name returns the module's name.
func (m *Module) Name() string {
	return "base64"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	mod := l.SetFuncs(engine.NewTable(2), map[string]lua.LGFunction{
		"encode": m.encode,
		"decode": m.decode,
	})
	l.Push(mod)
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
		// Base64 decoding errors - return nil and error
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(decoded))
	return 1
}
