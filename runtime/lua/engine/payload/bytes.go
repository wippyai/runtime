package payload

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// RegisterBytes registers Bytes<->Lua transcoders
func RegisterBytes(transcoder payload.TranscoderRegister) {
	to := &BytesToLua{}
	from := &ToBytes{}

	transcoder.RegisterTranscoder(payload.Bytes, payload.Lua, 1, to)
	transcoder.RegisterTranscoder(payload.Lua, payload.Bytes, 1, from)
}

// RegisterAllBasicFormats registers String, Bytes, JSON and Golang<->Lua transcoders
func RegisterAllBasicFormats(transcoder payload.TranscoderRegister) {
	RegisterString(transcoder)
	RegisterBytes(transcoder)
	RegisterJSON(transcoder)
	Register(transcoder) // Registers Golang<->Lua
}

// BytesToLua converts a Bytes payload to a Lua payload
type BytesToLua struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *BytesToLua) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Bytes {
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("Bytes=>Lua can only transcode from Bytes format, got %s", p.Format()))
	}

	var bytes []byte
	switch v := p.Data().(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("Bytes=>Lua can only handle []byte or string, got %T", p.Data()))
	}

	// Create a new Lua string from the bytes
	lv := lua.LString(bytes)
	return payload.NewPayload(lv, payload.Lua), nil
}

// ToBytes converts a Lua payload to a Bytes payload
type ToBytes struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *ToBytes) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("Lua=>Bytes can only transcode from Lua format, got %s", p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("Lua=>Bytes expects data to be of type lua.LValue, got %T", p.Data()))
	}

	// Handle different Lua types
	var result []byte
	//exhaustive:ignore
	switch lv.Type() {
	case lua.LTString:
		result = []byte(lv.(lua.LString))
	case lua.LTNumber:
		result = []byte(fmt.Sprintf("%v", float64(lv.(lua.LNumber))))
	case lua.LTBool:
		result = []byte(fmt.Sprintf("%t", bool(lv.(lua.LBool))))
	case lua.LTNil:
		// Nil to empty bytes
	case lua.LTTable:
		result = []byte(fmt.Sprintf("%v", value.ToGoAny(lv)))
	case lua.LTInteger:
		result = []byte(fmt.Sprintf("%d", int64(lv.(lua.LInteger))))
	default:
		result = []byte(lv.String())
	}

	return payload.NewPayload(result, payload.Bytes), nil
}
