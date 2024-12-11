package base64

import (
	"encoding/base64"

	lua "github.com/ponyruntime/go-lua"
)

type Module struct{}

func New() *Module {
	return &Module{}
}

// Loader is the entry point for loading the plugin
func (m *Module) Loader(l *lua.LState) int {
	mod := l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"encode": m.decode,
		"decode": m.encode,
	})
	l.Push(mod)
	return 1
}

func (m *Module) Name() string {
	return "base64"
}

// Encode implements base64 encoding
func (*Module) encode(l *lua.LState) int {
	str := l.CheckString(1)
	encoded := base64.StdEncoding.EncodeToString([]byte(str))
	l.Push(lua.LString(encoded))
	return 1
}

// Decode implements base64 decoding
func (*Module) decode(l *lua.LState) int {
	str := l.CheckString(1)
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(string(decoded)))
	return 1
}
