// SPDX-License-Identifier: MPL-2.0

package value

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-json-experiment/json/jsontext"
	lua "github.com/wippyai/go-lua"
)

type jsonNull struct{}

// jsonRaw is the Value of a raw JSON userdata. NewJSONRaw is its only
// constructor, so text is always one compact, valid JSON value.
type jsonRaw struct {
	text string
}

// JSONNull is the userdata that stands for a JSON null. It is a single value
// shared by every Lua state: it holds no state and has no metatable, so Lua
// code compares it by identity and cannot change it.
var JSONNull = &lua.LUserData{Value: jsonNull{}, Metatable: lua.LNil}

// NewJSONRaw returns a userdata holding text as raw JSON. text must be exactly
// one JSON value with unique member names, valid UTF-8 and numbers within
// float64 range. The stored text drops insignificant whitespace and keeps
// members, numbers and strings as written. The userdata has no metatable and
// immutable contents, so it is safe to share between Lua states.
func NewJSONRaw(text []byte) (*lua.LUserData, error) {
	value := jsontext.Value(text).Clone()
	if err := value.Compact(jsontext.AllowDuplicateNames(false), jsontext.AllowInvalidUTF8(false)); err != nil {
		return nil, err
	}
	if _, err := jsonRawToGo(string(value)); err != nil {
		return nil, err
	}
	return &lua.LUserData{Value: &jsonRaw{text: string(value)}, Metatable: lua.LNil}, nil
}

// JSONRawText returns the JSON text held by a raw JSON userdata.
func JSONRawText(v lua.LValue) (string, bool) {
	ud, ok := v.(*lua.LUserData)
	if !ok {
		return "", false
	}
	raw, ok := ud.Value.(*jsonRaw)
	if !ok {
		return "", false
	}
	return raw.text, true
}

// IsJSONValue reports whether v is JSONNull or a raw JSON userdata. Both are
// immutable, so they cross Lua states by reference.
func IsJSONValue(v lua.LValue) bool {
	if v == lua.LValue(JSONNull) {
		return true
	}
	_, ok := JSONRawText(v)
	return ok
}

// jsonRawToGo decodes JSON text into the Go values ToGoAny produces for the
// equivalent Lua value: integers that fit int64 become int64 and every other
// number becomes float64, as the json module decodes them.
func jsonRawToGo(text string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return convertJSONNumbers(decoded)
}

func convertJSONNumbers(v any) (any, error) {
	switch value := v.(type) {
	case json.Number:
		text := value.String()
		if !strings.ContainsAny(text, ".eE") {
			if n, err := value.Int64(); err == nil {
				return n, nil
			}
		}
		f, err := value.Float64()
		if err != nil {
			return nil, fmt.Errorf("number %s: %w", text, err)
		}
		return f, nil
	case []any:
		for i, item := range value {
			converted, err := convertJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[i] = converted
		}
		return value, nil
	case map[string]any:
		for key, item := range value {
			converted, err := convertJSONNumbers(item)
			if err != nil {
				return nil, err
			}
			value[key] = converted
		}
		return value, nil
	default:
		return value, nil
	}
}

// jsonRawGoValue decodes the text of a raw JSON userdata. The text was decoded
// by NewJSONRaw, so a failure here is a broken invariant.
func jsonRawGoValue(text string) any {
	decoded, err := jsonRawToGo(text)
	if err != nil {
		panic(fmt.Sprintf("value: raw JSON userdata holds undecodable text: %v", err))
	}
	return decoded
}
