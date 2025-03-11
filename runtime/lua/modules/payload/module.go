package payload

import (
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine/errors"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const (
	// TypeName is the type name for payload userdata in Lua
	TypeName = "payload"
)

// Module provides payload operations with lazy transcoding
type Module struct {
}

// NewPayloadModule creates a new payload module for Lua
func NewPayloadModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "payload"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	value.RegisterTypeMethods(l, TypeName, nil, map[string]lua.LGFunction{
		"get_format": m.payloadFormat,
		"data":       m.payloadData,
		"transcode":  m.payloadTranscode,
		"unmarshal":  m.payloadUnmarshal,
	})

	mod := l.CreateTable(0, 2)
	mod.RawSetString("new", l.NewFunction(m.newPayload))

	formats := l.CreateTable(0, 7)
	formats.RawSetString("JSON", lua.LString(payload.JSON))
	formats.RawSetString("YAML", lua.LString(payload.YAML))
	formats.RawSetString("STRING", lua.LString(payload.String))
	formats.RawSetString("GOLANG", lua.LString(payload.Golang))
	formats.RawSetString("LUA", lua.LString(payload.Lua))
	formats.RawSetString("BYTES", lua.LString(payload.Bytes))
	formats.RawSetString("ERROR", lua.LString(payload.Error))
	mod.RawSetString("format", formats)

	l.Push(mod)
	return 1
}

type Wrapper struct {
	Payload payload.Payload
}

// newPayload creates a new payload from Lua value and format
// Params: value, format
// Returns: payload userdata
func (m *Module) newPayload(l *lua.LState) int {
	v := l.Get(1)

	if err := errors.Unwrap(v); err != nil {
		p := payload.NewPayload(err, payload.Error)
		return PushPayload(l, p)
	}

	p := payload.NewPayload(v, payload.Lua)
	return PushPayload(l, p)
}

// payloadFormat returns the format of a payload
// Method: payload:format()
// Returns: format string
func (m *Module) payloadFormat(l *lua.LState) int {
	p := CheckPayload(l)
	l.Push(lua.LString(p.Payload.Format()))
	return 1
}

// payloadData returns the raw data from a payload without transcoding
// Method: payload:data()
// Returns: data (if already in Lua format) or nil
func (m *Module) payloadData(l *lua.LState) int {
	p := CheckPayload(l)
	if p.Payload.Format() == payload.Lua {
		// Data is already in Lua format, return it directly
		if lv, ok := p.Payload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	// Transcode to Lua format
	luaPayload, err := dtt.Transcode(p.Payload, payload.Lua)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Extract the Lua value
	if lv, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	l.Push(lua.LNil)
	return 1
}

// payloadTranscode transcodes a payload to a new format
// Method: payload:transcode(format)
// Returns: new payload userdata, error
func (m *Module) payloadTranscode(l *lua.LState) int {
	p := CheckPayload(l)
	format := payload.Format(l.CheckString(2))

	// Get transcoder from context
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	// Transcode the payload
	result, err := dtt.Transcode(p.Payload, format)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	PushPayload(l, result)
	return 1
}

// payloadUnmarshal transcodes a payload to Lua format and returns the data
// Method: payload:unmarshal()
// Returns: lua value, error
func (m *Module) payloadUnmarshal(l *lua.LState) int {
	p := CheckPayload(l)

	// If already in Lua format, return the data directly
	if p.Payload.Format() == payload.Lua {
		if lv, ok := p.Payload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	// Get transcoder from context
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.RaiseError("transcoder not found")
		return 0
	}

	// Transcode to Lua format
	luaPayload, err := dtt.Transcode(p.Payload, payload.Lua)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Extract the Lua value
	if lv, ok := luaPayload.Data().(lua.LValue); ok {
		l.Push(lv)
		return 1
	}

	// If not a valid Lua value, return nil and error
	l.Push(lua.LNil)
	l.Push(lua.LString("transcoded data is not a valid Lua value"))
	return 2
}

// Helper functions

// CheckPayload gets a payload wrapper from the Lua stack
func CheckPayload(l *lua.LState) *Wrapper {
	ud := l.CheckUserData(1)
	if pw, ok := ud.Value.(*Wrapper); ok {
		return pw
	}
	l.ArgError(1, "payload expected")
	return nil
}

// PushPayload creates a payload userdata and pushes it onto the stack
// Returns 1 (number of values pushed)
func PushPayload(l *lua.LState, p payload.Payload) int {
	ud := l.NewUserData()
	ud.Value = &Wrapper{Payload: p}
	ud.Metatable = value.GetTypeMetatable(l, TypeName)
	l.Push(ud)
	return 1
}

func WrapPayload(l *lua.LState, p payload.Payload) lua.LValue {
	ud := l.NewUserData()
	ud.Value = &Wrapper{Payload: p}
	ud.Metatable = value.GetTypeMetatable(l, TypeName)

	return ud
}
