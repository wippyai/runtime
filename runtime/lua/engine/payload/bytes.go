package payload

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
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
		return nil, fmt.Errorf("Bytes=>Lua can only transcode from Bytes format, got %s", p.Format())
	}

	var bytes []byte
	switch v := p.Data().(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return nil, fmt.Errorf("Bytes=>Lua can only handle []byte or string, got %T", p.Data())
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
		return nil, fmt.Errorf("Lua=>Bytes can only transcode from Lua format, got %s", p.Format())
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, fmt.Errorf("Lua=>Bytes expects data to be of type lua.LValue, got %T", p.Data())
	}

	// Handle different Lua types
	var result []byte
	switch lv.Type() {
	case lua.LTString:
		// Direct conversion to bytes
		result = []byte(lv.(lua.LString))
	case lua.LTNumber:
		// Number to string to bytes
		result = []byte(fmt.Sprintf("%v", float64(lv.(lua.LNumber))))
	case lua.LTBool:
		// Boolean to string to bytes
		result = []byte(fmt.Sprintf("%t", bool(lv.(lua.LBool))))
	case lua.LTNil:
		// Nil to empty bytes
	case lua.LTTable:
		// For tables, convert to string representation first
		result = []byte(fmt.Sprintf("%v", value.ToGoAny(lv)))
	case lua.LTInteger:
		// Integer to string to bytes
		result = []byte(fmt.Sprintf("%d", int64(lv.(lua.LInteger))))
	case lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		// For other types, use string representation
		result = []byte(lv.String())
	}

	return payload.NewPayload(result, payload.Bytes), nil
}
