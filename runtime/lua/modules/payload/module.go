package payload

import (
	"sync"

	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua2api "github.com/wippyai/runtime/api/runtime/lua2"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	typeName = "payload"
)

var (
	moduleTable      *lua.LTable
	registration     *lua2api.Registration
	payloadMetatable *lua.LTable
	initOnce         sync.Once
)

// Module is the singleton payload module instance.
var Module = &payloadModule{}

type payloadModule struct{}

func (m *payloadModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "payload",
		Description: "Payload transcoding and format conversion",
		Class:       []string{luaapi.ClassEncoding, luaapi.ClassDeterministic},
	}
}

func (m *payloadModule) Register(l *lua.LState) *lua2api.Registration {
	initOnce.Do(func() {
		moduleTable = createModuleTable()

		payloadMetatable = value.RegisterTypeMethods(nil, typeName,
			map[string]lua.LGFunction{"__tostring": payloadToString},
			payloadMethods)

		registration = &lua2api.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	return registration
}

type Wrapper struct {
	Payload payload.Payload
}

func (m *payloadModule) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use lua2api.LoadModule(l, Module) instead.
func Bind(l *lua.LState) {
	lua2api.LoadModule(l, Module)
}

func createModuleTable() *lua.LTable {
	mod := lua.CreateTable(0, 2)

	mod.RawSetString("new", lua.LGoFunc(newPayload))

	formats := lua.CreateTable(0, 7)
	formats.RawSetString("JSON", lua.LString(payload.JSON))
	formats.RawSetString("YAML", lua.LString(payload.YAML))
	formats.RawSetString("STRING", lua.LString(payload.String))
	formats.RawSetString("GOLANG", lua.LString(payload.Golang))
	formats.RawSetString("LUA", lua.LString(payload.Lua))
	formats.RawSetString("BYTES", lua.LString(payload.Bytes))
	formats.RawSetString("ERROR", lua.LString(payload.Error))
	formats.Immutable = true
	mod.RawSetString("format", formats)

	mod.Immutable = true
	return mod
}

var payloadMethods = map[string]lua.LGFunction{
	"get_format": payloadGetFormat,
	"data":       payloadData,
	"transcode":  payloadTranscode,
	"unmarshal":  payloadUnmarshal,
}

func newPayload(l *lua.LState) int {
	v := l.Get(1)

	// Check if the value is an error
	if err := lua.ExtractError(v); err != nil {
		p := payload.NewPayload(err, payload.Error)
		return PushPayload(l, p)
	}

	p := payload.NewPayload(v, payload.Lua)
	return PushPayload(l, p)
}

func checkPayload(l *lua.LState, idx int) *Wrapper {
	ud := l.CheckUserData(idx)
	if pw, ok := ud.Value.(*Wrapper); ok {
		return pw
	}
	l.ArgError(idx, "payload expected")
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
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
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

	format := payload.Format(l.CheckString(2))
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
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	PushPayload(l, result)
	return 1
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
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	if lv, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	l.Push(lua.LNil)
	l.Push(lua.LString("transcoded data is not a valid Lua value"))
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
	value.NewUserData(l, &Wrapper{Payload: p}, payloadMetatable)
	return 1
}

func WrapPayload(l *lua.LState, p payload.Payload) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &Wrapper{Payload: p}
	ud.Metatable = payloadMetatable
	return ud
}
