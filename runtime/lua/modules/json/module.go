// SPDX-License-Identifier: MPL-2.0

package json

import (
	lua "github.com/wippyai/go-lua"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luavalue "github.com/wippyai/runtime/runtime/lua/engine/value"
)

// Module is the json module definition.
var Module = &luaapi.ModuleDef{
	Name:        "json",
	Description: "JSON encoding and decoding with schema validation",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		initSchemaCache()

		mod := lua.CreateTable(0, 7)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.RawSetString("decode_with_nulls", lua.LGoFunc(decodeWithNullsFunc))
		mod.RawSetString("raw", lua.LGoFunc(rawFunc))
		mod.RawSetString("null", luavalue.JSONNull)
		mod.RawSetString("validate", lua.LGoFunc(validateFunc))
		mod.RawSetString("validate_string", lua.LGoFunc(validateStringFunc))
		mod.Immutable = true
		return mod, nil
	},
	Types: ModuleTypes,
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
		l.Push(lua.LNil)
		return 2
	}

	data, err := Encode(value)
	if err != nil {
		return internalError(l, err, "encode failed")
	}
	l.Push(lua.LString(data))
	l.Push(lua.LNil)
	return 2
}

// jsonTextArg reads the JSON text argument of decode and decode_with_nulls.
// When the argument is invalid it pushes the error results and returns their
// count; otherwise the count is zero.
func jsonTextArg(l *lua.LState) (lua.LString, int) {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return "", invalidInputError(l, "string expected")
	}
	if str == "" {
		return "", invalidInputError(l, "empty string is not valid JSON")
	}
	return str, 0
}

func pushDecoded(l *lua.LState, decode func([]byte) (lua.LValue, error), str lua.LString) int {
	value, err := decode([]byte(str))
	if err != nil {
		return internalError(l, err, "decode failed")
	}
	l.Push(value)
	l.Push(lua.LNil)
	return 2
}

func decodeFunc(l *lua.LState) int {
	str, n := jsonTextArg(l)
	if n > 0 {
		return n
	}
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		target, ok := l.Get(2).(*lua.LType)
		if !ok {
			return invalidInputError(l, "$: expected type value")
		}
		value, err := decodeTypedString(string(str), target, l)
		if err != nil {
			return invalidInputError(l, err.Error())
		}
		l.Push(value)
		l.Push(lua.LNil)
		return 2
	}
	return pushDecoded(l, Decode, str)
}

func decodeWithNullsFunc(l *lua.LState) int {
	str, n := jsonTextArg(l)
	if n > 0 {
		return n
	}
	return pushDecoded(l, DecodeWithNulls, str)
}

func rawFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return invalidInputError(l, "string expected")
	}

	raw, err := luavalue.NewJSONRaw([]byte(str))
	if err != nil {
		return invalidInputError(l, "invalid JSON: "+err.Error())
	}
	l.Push(raw)
	l.Push(lua.LNil)
	return 2
}

func validateFunc(l *lua.LState) int {
	return schemaValidateFunc(l)
}

func validateStringFunc(l *lua.LState) int {
	return schemaValidateStringFunc(l)
}
