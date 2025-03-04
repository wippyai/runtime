package payload

import (
	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	// TypeName is the type name for payload userdata in Lua
	PayloadTypeName = "payload"
)

// Module provides payload operations with lazy transcoding
type Module struct {
	log *zap.Logger
}

// NewPayloadModule creates a new payload module for Lua
func NewPayloadModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "payload"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create the payload metatable with methods
	mt := l.NewTypeMetatable(PayloadTypeName)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"format":    m.payloadFormat,
		"data":      m.payloadData,
		"transcode": m.payloadTranscode,
		"unmarshal": m.payloadUnmarshal,
	}))

	// Module functions table - optimized size
	mod := l.CreateTable(0, 5)
	mod.RawSetString("new", l.NewFunction(m.newPayload))
	mod.RawSetString("new_string", l.NewFunction(m.newStringPayload))
	mod.RawSetString("new_error", l.NewFunction(m.newErrorPayload))
	mod.RawSetString("is_payload", l.NewFunction(m.isPayload))

	// Format constants table - optimized size
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

// Userdata wrapper for payloads
type PayloadWrapper struct {
	Payload payload.Payload
}

// newPayload creates a new payload from Lua value and format
// Params: value, format
// Returns: payload userdata
func (m *Module) newPayload(l *lua.LState) int {
	value := l.Get(1)
	format := payload.Format(l.CheckString(2))

	p := payload.NewPayload(value, format)
	return pushPayload(l, p)
}

// newStringPayload creates a new string payload
// Params: string
// Returns: payload userdata
func (m *Module) newStringPayload(l *lua.LState) int {
	str := l.CheckString(1)
	p := payload.NewString(str)
	return pushPayload(l, p)
}

// newErrorPayload creates a new error payload
// Params: error_message
// Returns: payload userdata
func (m *Module) newErrorPayload(l *lua.LState) int {
	errMsg := l.CheckString(1)
	p := payload.NewError(errorString(errMsg))
	return pushPayload(l, p)
}

// errorString is a simple error type
type errorString string

func (e errorString) Error() string {
	return string(e)
}

// isPayload checks if a value is a payload userdata
// Params: value
// Returns: boolean
func (m *Module) isPayload(l *lua.LState) int {
	value := l.Get(1)
	if ud, ok := value.(*lua.LUserData); ok {
		_, ok := ud.Value.(*PayloadWrapper)
		l.Push(lua.LBool(ok))
	} else {
		l.Push(lua.LBool(false))
	}
	return 1
}

// payloadFormat returns the format of a payload
// Method: payload:format()
// Returns: format string
func (m *Module) payloadFormat(l *lua.LState) int {
	p := checkPayload(l)
	l.Push(lua.LString(p.Payload.Format()))
	return 1
}

// payloadData returns the raw data from a payload without transcoding
// Method: payload:data()
// Returns: data (if already in Lua format) or nil
func (m *Module) payloadData(l *lua.LState) int {
	p := checkPayload(l)
	if p.Payload.Format() == payload.Lua {
		// Data is already in Lua format, return it directly
		if lv, ok := p.Payload.Data().(lua.LValue); ok {
			l.Push(lv)
			return 1
		}
	}

	// Data is not in Lua format, return nil
	l.Push(lua.LNil)
	return 1
}

// payloadTranscode transcodes a payload to a new format
// Method: payload:transcode(format)
// Returns: new payload userdata, error
func (m *Module) payloadTranscode(l *lua.LState) int {
	p := checkPayload(l)
	format := payload.Format(l.CheckString(2))

	// Get transcoder from context
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no transcoder found in context"))
		return 2
	}

	// Transcode the payload
	result, err := dtt.Transcode(p.Payload, format)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	pushPayload(l, result)
	return 1
}

// payloadUnmarshal transcodes a payload to Lua format and returns the data
// Method: payload:unmarshal()
// Returns: lua value, error
func (m *Module) payloadUnmarshal(l *lua.LState) int {
	p := checkPayload(l)

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
		l.Push(lua.LNil)
		l.Push(lua.LString("no transcoder found in context"))
		return 2
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
func checkPayload(l *lua.LState) *PayloadWrapper {
	ud := l.CheckUserData(1)
	if pw, ok := ud.Value.(*PayloadWrapper); ok {
		return pw
	}
	l.ArgError(1, "payload expected")
	return nil
}

// PushPayload pushes a payload onto the Lua stack as userdata
func pushPayload(l *lua.LState, p payload.Payload) int {
	ud := l.NewUserData()
	ud.Value = &PayloadWrapper{Payload: p}
	l.SetMetatable(ud, l.GetTypeMetatable(PayloadTypeName))
	l.Push(ud)
	return 1
}
