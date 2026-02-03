package payload

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

// RegisterString registers String<->Lua transcoders
func RegisterString(transcoder payload.TranscoderRegister) {
	to := &StringToLua{}
	from := &ToString{}

	transcoder.RegisterTranscoder(payload.String, payload.Lua, 1, to)
	transcoder.RegisterTranscoder(payload.Lua, payload.String, 1, from)
}

// StringToLua converts a String payload to a Lua payload
type StringToLua struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *StringToLua) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.String {
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("String=>Lua can only transcode from String format, got %s", p.Format()))
	}

	var str string
	switch v := p.Data().(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("String=>Lua can only handle string or []byte, got %T", p.Data()))
	}

	// Convert the string to a Lua string value
	lv := lua.LString(str)
	return payload.NewPayload(lv, payload.Lua), nil
}

// ToString converts a Lua payload to a String payload
type ToString struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *ToString) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("Lua=>String can only transcode from Lua format, got %s", p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("Lua=>String expects data to be of type lua.LValue, got %T", p.Data()))
	}

	// Handle different Lua types
	var result string
	switch lv.Type() {
	case lua.LTString:
		// Direct string conversion
		result = string(lv.(lua.LString))
	case lua.LTNumber:
		// Number to string
		result = fmt.Sprintf("%v", float64(lv.(lua.LNumber)))
	case lua.LTBool:
		// Boolean to string
		result = fmt.Sprintf("%t", bool(lv.(lua.LBool)))
	case lua.LTNil:
		// Nil to empty string
		result = ""
	case lua.LTTable:
		// For tables, use a simplified string representation
		result = fmt.Sprintf("%v", value.ToGoAny(lv))
	case lua.LTInteger:
		// Integer to string
		result = fmt.Sprintf("%d", int64(lv.(lua.LInteger)))
	case lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		fallthrough
	default:
		// For other types, just use the string representation
		result = lv.String()
	}

	return payload.NewPayload(result, payload.String), nil
}
