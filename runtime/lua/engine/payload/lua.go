// SPDX-License-Identifier: MPL-2.0

package payload

import (
	jsongo "encoding/json"
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
)

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
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("Lua=>Golang can only transcode from Lua format, got %s", p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data()))
	}

	data := value.ToGoAny(lv)

	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v any) error {
	if p.Format() != payload.Lua {
		return runtimelua.NewInvalidFormatError(fmt.Sprintf("Lua=>Golang can only unmarshal from Lua format, got %s", p.Format()))
	}

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return runtimelua.NewInvalidTypeError(fmt.Sprintf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data()))
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
	return t.TranscodeWith(nil, p)
}

// TranscodeWith implements payload.ContextFormatTranscoder.
func (t *FromGolang) TranscodeWith(tc *payload.TranscodeContext, p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, runtimelua.NewInvalidFormatError(fmt.Sprintf("Golang=>Lua can only transcode from Golang format, got %s", p.Format()))
	}

	converter := goToLuaConverter{context: tc, normalize: true}
	lv, err := converter.convert(p.Data())
	if err != nil {
		return nil, err
	}

	return payload.NewPayload(lv, payload.Lua), nil
}

func normalizeNestedPayload(tc *payload.TranscodeContext, pl payload.Payload) (any, error) {
	if pl == nil {
		return nil, nil
	}

	if pl.Format() == payload.Lua {
		if lv, ok := pl.Data().(lua.LValue); ok {
			return lv, nil
		}
		return pl.Data(), nil
	}

	if tc != nil && tc.Parent != nil {
		luaPayload, err := tc.Parent.Transcode(pl, payload.Lua)
		if err != nil {
			return nil, err
		}
		if luaPayload == nil {
			return nil, nil
		}
		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			return lv, nil
		}
		return nil, runtimelua.NewInvalidTypeError(fmt.Sprintf("payload transcoded to Lua must contain lua.LValue, got %T", luaPayload.Data()))
	}

	// Legacy fallback for low-level direct transcoder use without parent context.
	return pl.Data(), nil
}
