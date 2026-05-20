// SPDX-License-Identifier: MPL-2.0

package payload

import (
	jsongo "encoding/json"
	"fmt"
	"reflect"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	jsonlua "github.com/wippyai/runtime/runtime/lua/modules/json"
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

	normalized, err := normalizeForLua(tc, p.Data())
	if err != nil {
		return nil, err
	}

	lv, err := GoToLua(normalized)
	if err != nil {
		return nil, err
	}

	return payload.NewPayload(lv, payload.Lua), nil
}

func normalizeForLua(tc *payload.TranscodeContext, v any) (any, error) {
	if v == nil {
		return nil, nil
	}

	switch val := v.(type) {
	case payload.Payload:
		return normalizeNestedPayload(tc, val)
	case lua.LValue:
		return val, nil
	case pid.PID:
		return val.String(), nil
	case *pid.PID:
		if val == nil {
			return nil, nil
		}
		return val.String(), nil
	case time.Time:
		return val, nil
	case *time.Time:
		if val == nil {
			return nil, nil
		}
		return *val, nil
	case time.Duration:
		return int64(val), nil
	case *time.Duration:
		if val == nil {
			return nil, nil
		}
		return int64(*val), nil
	case error:
		return val, nil
	}

	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return nil, nil
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return nil, nil
		}
		return normalizeForLua(tc, rv.Elem().Interface())

	case reflect.Interface:
		if rv.IsNil() {
			return nil, nil
		}
		return normalizeForLua(tc, rv.Elem().Interface())

	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}, nil
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			normalized, err := normalizeForLua(tc, iter.Value().Interface())
			if err != nil {
				return nil, err
			}
			out[fmt.Sprint(iter.Key().Interface())] = normalized
		}
		return out, nil

	case reflect.Slice:
		// Keep []byte behavior as Lua string.
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return v, nil
		}
		if rv.IsNil() {
			return nil, nil
		}
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			normalized, err := normalizeForLua(tc, rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil

	case reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			normalized, err := normalizeForLua(tc, rv.Index(i).Interface())
			if err != nil {
				return nil, err
			}
			out[i] = normalized
		}
		return out, nil

	case reflect.Struct:
		fields := getStructFields(rv.Type())
		out := make(map[string]any, len(fields))
		for _, field := range fields {
			fieldValue := rv.Field(field.index)
			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					out[field.name] = map[string]any{}
					continue
				}
			case reflect.Pointer, reflect.Slice, reflect.Interface:
				if fieldValue.IsNil() {
					out[field.name] = nil
					continue
				}
			}

			normalized, err := normalizeForLua(tc, fieldValue.Interface())
			if err != nil {
				return nil, err
			}
			out[field.name] = normalized
		}
		return out, nil
	}

	return v, nil
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
