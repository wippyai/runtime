package payload

import (
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	lua "github.com/yuin/gopher-lua"
)

// RegisterJSON registers JSON<->Lua transcoders
func RegisterJSON(transcoder payload.TranscoderRegister) {
	to := &JSONToLua{}
	from := &ToJSON{}

	transcoder.RegisterTranscoder(payload.JSON, payload.Lua, 1, to)
	transcoder.RegisterTranscoder(payload.Lua, payload.JSON, 1, from)
}

// JSONToLua converts a JSON payload to a Lua payload
type JSONToLua struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *JSONToLua) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.JSON {
		return nil, luaapi.NewInvalidFormatError(fmt.Sprintf("JSON=>Lua can only transcode from JSON format, got %s", p.Format()))
	}

	var data []byte
	switch v := p.Data().(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return nil, luaapi.NewInvalidTypeError(fmt.Sprintf("JSON=>Lua can only handle string or []byte, got %T", p.Data()))
	}

	luaValue, err := json.Decode(data)
	if err != nil {
		return nil, runtimelua.NewTranscodeError("failed to decode JSON", err)
	}

	return payload.NewPayload(luaValue, payload.Lua), nil
}

// ToJSON converts a Lua payload to a JSON payload
type ToJSON struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *ToJSON) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, luaapi.NewInvalidFormatError(fmt.Sprintf("Lua=>JSON can only transcode from Lua format, got %s", p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, luaapi.NewInvalidTypeError(fmt.Sprintf("Lua=>JSON expects data to be of type lua.LValue, got %T", p.Data()))
	}

	jsonData, err := json.Encode(lv)
	if err != nil {
		return nil, runtimelua.NewTranscodeError("failed to encode to JSON", err)
	}

	return payload.NewPayload(jsonData, payload.JSON), nil
}
