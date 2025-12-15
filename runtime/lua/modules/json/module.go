package json

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module is the json module definition.
var Module = &luaapi.ModuleDef{
	Name:        "json",
	Description: "JSON encoding and decoding with schema validation",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initSchemaCache()

		mod := lua.CreateTable(0, 4)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.RawSetString("validate", lua.LGoFunc(validateFunc))
		mod.RawSetString("validate_string", lua.LGoFunc(validateStringFunc))
		mod.Immutable = true
		return mod, nil
	},
}

func invalidInputError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func encodeFunc(l *lua.LState) int {
	value := l.Get(1)
	if value == lua.LNil {
		l.Push(lua.LString("null"))
		return 1
	}

	data, err := Encode(value)
	if err != nil {
		return internalError(l, err, "encode failed")
	}
	l.Push(lua.LString(data))
	return 1
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return invalidInputError(l, "string expected")
	}

	if str == "" {
		return invalidInputError(l, "empty string is not valid JSON")
	}

	value, err := Decode([]byte(str))
	if err != nil {
		return internalError(l, err, "decode failed")
	}
	l.Push(value)
	return 1
}

func validateFunc(l *lua.LState) int {
	return schemaValidateFunc(l)
}

func validateStringFunc(l *lua.LState) int {
	return schemaValidateStringFunc(l)
}
