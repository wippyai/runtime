package payload

import (
	"errors"

	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	typeName = "payload"
)

// Module is the payload module definition.
var Module = &luaapi.ModuleDef{
	Name:        "payload",
	Description: "Payload transcoding and format conversion",
	Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	Build:       buildModule,
}

func init() {
	value.RegisterTypeMethods(nil, typeName,
		map[string]lua.LGoFunc{"__tostring": payloadToString},
		payloadMethods)
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 2)

	mod.RawSetString("new", lua.LGoFunc(newPayload))

	formats := lua.CreateTable(0, 8)
	formats.RawSetString("JSON", lua.LString(payload.JSON))
	formats.RawSetString("YAML", lua.LString(payload.YAML))
	formats.RawSetString("STRING", lua.LString(payload.String))
	formats.RawSetString("GOLANG", lua.LString(payload.Golang))
	formats.RawSetString("LUA", lua.LString(payload.Lua))
	formats.RawSetString("BYTES", lua.LString(payload.Bytes))
	formats.RawSetString("MSGPACK", lua.LString(payload.MsgPack))
	formats.RawSetString("ERROR", lua.LString(payload.GoError))
	formats.Immutable = true
	mod.RawSetString("format", formats)

	mod.Immutable = true
	return mod, nil
}

// Wrapper wraps a payload for Lua userdata.
type Wrapper struct {
	Payload payload.Payload
}

var payloadMethods = map[string]lua.LGoFunc{
	"get_format": payloadGetFormat,
	"data":       payloadData,
	"transcode":  payloadTranscode,
	"unmarshal":  payloadUnmarshal,
}

func newPayload(l *lua.LState) int {
	v := l.Get(1)

	// Check if the value is an error
	if err, isErr := v.(error); isErr {
		var luaErr *lua.Error
		if errors.As(err, &luaErr) {
			p := payload.NewPayload(luaErr, payload.GoError)
			return PushPayload(l, p)
		}
	}

	p := payload.NewPayload(v, payload.Lua)
	return PushPayload(l, p)
}

func checkPayload(l *lua.LState, _ int) *Wrapper {
	ud := l.CheckUserData(1)
	if pw, ok := ud.Value.(*Wrapper); ok {
		return pw
	}
	l.ArgError(1, "payload expected")
	return nil
}

func payloadGetFormat(l *lua.LState) int {
	p := checkPayload(l, 1)
	if p == nil {
		return 0
	}
	l.Push(lua.LString(p.Payload.Format()))
	return 1
}

func payloadData(l *lua.LState) int {
	p := checkPayload(l, 1)
	if p == nil {
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	if p.Payload.Format() == payload.Lua {
		if lv, ok := p.Payload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	tc := payload.GetTranscoder(ctx)
	if tc == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	luaPayload, err := tc.Transcode(p.Payload, payload.Lua)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "transcode failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if lv, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

func payloadTranscode(l *lua.LState) int {
	p := checkPayload(l, 1)
	if p == nil {
		return 0
	}

	format := l.CheckString(2)
	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	tc := payload.GetTranscoder(ctx)
	if tc == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	result, err := tc.Transcode(p.Payload, format)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "transcode failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	PushPayload(l, result)
	l.Push(lua.LNil)
	return 2
}

func payloadUnmarshal(l *lua.LState) int {
	p := checkPayload(l, 1)
	if p == nil {
		return 0
	}

	ctx := l.Context()
	if ctx == nil {
		l.RaiseError("no context")
		return 0
	}

	if p.Payload.Format() == payload.Lua {
		if lv, ok := p.Payload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	tc := payload.GetTranscoder(ctx)
	if tc == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	luaPayload, err := tc.Transcode(p.Payload, payload.Lua)
	if err != nil {
		luaErr := lua.WrapErrorWithLua(l, err, "unmarshal failed").
			WithKind(lua.Internal).
			WithRetryable(false)
		l.Push(lua.LNil)
		l.Push(luaErr)
		return 2
	}

	if lv, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	luaErr := lua.NewLuaError(l, "transcoded data is not a valid Lua value").
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(luaErr)
	return 2
}

func payloadToString(l *lua.LState) int {
	p := checkPayload(l, 1)
	if p == nil {
		return 0
	}
	l.Push(lua.LString("payload{format=" + string(p.Payload.Format()) + "}"))
	return 1
}

func PushPayload(l *lua.LState, p payload.Payload) int {
	value.PushTypedUserData(l, &Wrapper{Payload: p}, typeName)
	return 1
}

func WrapPayload(l *lua.LState, p payload.Payload) lua.LValue {
	return value.NewTypedUserData(l, &Wrapper{Payload: p}, typeName)
}
