package local

import (
	"encoding/base64"

	lua "git.spiralscout.com/estimation-engine/go-lua"
)

// Encode implements base64 encoding
func Encode(l *lua.LState) int {
	str := l.CheckString(1)
	encoded := base64.StdEncoding.EncodeToString([]byte(str))
	l.Push(lua.LString(encoded))
	return 1
}

// Decode implements base64 decoding
func Decode(l *lua.LState) int {
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

// Loader is the entry point for loading the plugin
func Loader(l *lua.LState) int {
	mod := l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"encode": Encode,
		"decode": Decode,
	})
	l.Push(mod)
	return 1
}
