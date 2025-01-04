package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/yuin/gopher-lua"
)

// RegisterJson registers JSON<->Lua transcoders
func RegisterJson(transcoder payload.TranscoderRegister) {
	to := &JsonToLua{}
	from := &LuaToJson{}

	transcoder.RegisterTranscoder(payload.Json, payload.Lua, 1, to)
	transcoder.RegisterTranscoder(payload.Lua, payload.Json, 1, from)
}

// JsonToLua converts a JSON payload to a Lua payload
type JsonToLua struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *JsonToLua) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Json {
		return nil, fmt.Errorf("Json=>Lua can only transcode from JSON format, got %s", p.Format())
	}

	l := lua.NewState()
	defer l.Close()

	var data []byte
	switch v := p.Data().(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return nil, fmt.Errorf("Json=>Lua can only handle string or []byte, got %T", p.Data())
	}

	// Use the existing Decode function
	luaValue, err := json.Decode(l, data)
	if err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	return payload.NewPayload(luaValue, payload.Lua), nil
}

// LuaToJson converts a Lua payload to a JSON payload
type LuaToJson struct{}

// Transcode implements the payload.FormatTranscoder interface
func (t *LuaToJson) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, fmt.Errorf("Lua=>Json can only transcode from Lua format, got %s", p.Format())
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, fmt.Errorf("Lua=>Json expects data to be of type lua.LValue, got %T", p.Data())
	}

	// Use the existing Encode function
	jsonData, err := json.Encode(lv)
	if err != nil {
		return nil, fmt.Errorf("failed to encode to JSON: %w", err)
	}

	return payload.NewPayload(jsonData, payload.Json), nil
}
