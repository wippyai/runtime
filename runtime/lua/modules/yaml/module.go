package yaml

import (
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
	"gopkg.in/yaml.v3"
)

// Module is the yaml module definition.
var Module = &luaapi.ModuleDef{
	Name:        "yaml",
	Description: "YAML encoding and decoding",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build: func() (*lua.LTable, []luaapi.YieldType) {
		mod := lua.CreateTable(0, 2)
		mod.RawSetString("encode", lua.LGoFunc(encodeFunc))
		mod.RawSetString("decode", lua.LGoFunc(decodeFunc))
		mod.Immutable = true
		return mod, nil
	},
}

func encodeFunc(l *lua.LState) int {
	if l.GetTop() < 1 {
		return invalidError(l, "table expected")
	}

	luaVal := l.Get(1)
	if luaVal.Type() != lua.LTTable {
		return invalidError(l, "table expected")
	}

	goVal := value.ToGoAny(luaVal)

	data, err := yaml.Marshal(goVal)
	if err != nil {
		return internalError(l, err, "encode failed")
	}

	l.Push(lua.LString(data))
	l.Push(lua.LNil)
	return 2
}

func decodeFunc(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		return invalidError(l, "string expected")
	}

	if str == "" {
		return invalidError(l, "input cannot be empty")
	}

	var data interface{}
	if err := yaml.Unmarshal([]byte(str), &data); err != nil {
		return internalError(l, err, "decode failed")
	}

	lv, err := luaconv.GoToLua(data)
	if err != nil {
		return internalError(l, err, "convert to Lua failed")
	}

	l.Push(lv)
	l.Push(lua.LNil)
	return 2
}

func invalidError(l *lua.LState, msg string) int {
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
