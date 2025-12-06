//go:build !goexperiment.jsonv2

package json

import (
	"bytes"
	"encoding/json"

	lua "github.com/yuin/gopher-lua"
)

// Decode parses JSON data and returns a Lua value.
// This is the v1 implementation using encoding/json with intermediate Go types.
func Decode(data []byte) (lua.LValue, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return DecodeValue(value), nil
}

// DecodeValue converts Go value to Lua value with proper indexing.
func DecodeValue(value any) lua.LValue {
	switch converted := value.(type) {
	case bool:
		return lua.LBool(converted)
	case json.Number:
		if i, err := converted.Int64(); err == nil {
			return lua.LInteger(i)
		}
		if f, err := converted.Float64(); err == nil {
			return lua.LNumber(f)
		}
		return lua.LString(converted.String())
	case string:
		return lua.LString(converted)
	case []any:
		arr := lua.CreateTable(len(converted), 0)
		for i, item := range converted {
			arr.RawSetInt(i+1, DecodeValue(item))
		}
		return arr
	case map[string]any:
		tbl := lua.CreateTable(0, len(converted))
		for key, item := range converted {
			tbl.RawSetH(lua.LString(key), DecodeValue(item))
		}
		return tbl
	case nil:
		return lua.LNil
	}
	return lua.LNil
}
