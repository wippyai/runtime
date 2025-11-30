package payload

import (
	jsongo "encoding/json"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
	lua "github.com/yuin/gopher-lua"
)

// can be optimized

// Register registers the Lua transcoders.
func Register(transcoder payload.TranscoderRegister) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.Lua, payload.Golang, 2, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.Lua, 2, from)
	transcoder.RegisterUnmarshaler(payload.Lua, to)

	RegisterString(transcoder)
	RegisterBytes(transcoder)
	RegisterJSON(transcoder)
}

// ToGolang converts a Lua payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for Lua payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, fmt.Errorf("Lua=>Golang can only transcode from Lua format, got %s", p.Format())
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, fmt.Errorf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data())
	}

	data := value.ToGoAny(lv)

	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.Lua {
		return fmt.Errorf("Lua=>Golang can only unmarshal from Lua format, got %s", p.Format())
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return fmt.Errorf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data())
	}

	json, err := jsonlua.Encode(lv)
	if err != nil {
		return err
	}

	// but it works and respecs all the configs!
	return jsongo.Unmarshal(json, v)
}

// FromGolang converts a Golang payload to a Lua payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, fmt.Errorf("Golang=>Lua can only transcode from Golang format, got %s", p.Format())
	}

	lv, err := GoToLua(p.Data())
	if err != nil {
		return nil, err
	}

	return payload.NewPayload(lv, payload.Lua), nil
}
