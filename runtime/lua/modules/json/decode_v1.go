//go:build !goexperiment.jsonv2

package json

import (
	"bytes"
	"encoding/json"
	"errors"

	lua "github.com/wippyai/go-lua"
)

// maxNestingDepth limits JSON nesting to prevent stack overflow attacks.
const maxNestingDepth = 128

// ErrMaxDepthExceeded is returned when JSON nesting exceeds the limit.
var ErrMaxDepthExceeded = errors.New("json: maximum nesting depth exceeded")

// Decode parses JSON data and returns a Lua value.
// This is the v1 implementation using encoding/json with intermediate Go types.
func Decode(data []byte) (lua.LValue, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return decodeValueWithDepth(value, 0)
}

// decodeValueWithDepth converts Go value to Lua value with depth tracking.
func decodeValueWithDepth(value any, depth int) (lua.LValue, error) {
	if depth > maxNestingDepth {
		return nil, ErrMaxDepthExceeded
	}

	switch converted := value.(type) {
	case bool:
		return lua.LBool(converted), nil
	case json.Number:
		if i, err := converted.Int64(); err == nil {
			return lua.LInteger(i), nil
		}
		if f, err := converted.Float64(); err == nil {
			return lua.LNumber(f), nil
		}
		return lua.LString(converted.String()), nil
	case string:
		return lua.LString(converted), nil
	case []any:
		arr := lua.CreateTable(len(converted), 0)
		for i, item := range converted {
			val, err := decodeValueWithDepth(item, depth+1)
			if err != nil {
				return nil, err
			}
			arr.RawSetInt(i+1, val)
		}
		return arr, nil
	case map[string]any:
		tbl := lua.CreateTable(0, len(converted))
		for key, item := range converted {
			val, err := decodeValueWithDepth(item, depth+1)
			if err != nil {
				return nil, err
			}
			tbl.RawSetH(lua.LString(key), val)
		}
		return tbl, nil
	case nil:
		return lua.LNil, nil
	}
	return lua.LNil, nil
}
