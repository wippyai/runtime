package base64

import (
	"encoding/base64"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/wippyai/go-lua"
)

// Module is the base64 module definition.
var Module = &luaapi.ModuleDef{
	Name:        "base64",
	Description: "Base64 encoding and decoding",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
}

func encodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		err := lua.NewLuaError(l, "string expected").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(str))
	l.Push(lua.LString(encoded))
	return 1
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		err := lua.NewLuaError(l, "string expected").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	if str == "" {
		l.Push(lua.LString(""))
		return 1
	}

	decoded, decErr := base64.StdEncoding.DecodeString(string(str))
	if decErr != nil {
		err := lua.WrapErrorWithLua(l, decErr, "decode failed").
			WithKind(lua.Invalid).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(err)
		return 2
	}

	l.Push(lua.LString(decoded))
	return 1
}
