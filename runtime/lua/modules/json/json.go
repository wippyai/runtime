package json

import (
	"encoding/json"
	"errors"

	"github.com/yuin/gopher-lua"
)

var (
	errNested      = errors.New("cannot encode recursively nested tables to JSON")
	errSparseArray = errors.New("cannot encode sparse array")
	errInvalidKeys = errors.New("cannot encode mixed or invalid key types")
)

type Module struct{}

// NewJsonModule creates a new JSON module.
func NewJsonModule() *Module {
	return &Module{}
}

// Name returns the module name.
func (m *Module) Name() string {
	return "json"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()
	l.SetFuncs(t, map[string]lua.LGFunction{
		"decode": m.decode,
		"encode": m.encode,
	})
	l.Push(t)
	return 1
}

// decode decodes JSON string to Lua value with input validation.
func (*Module) decode(l *lua.LState) int {
	// Input validation errors - use ArgError
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	// Empty string is not valid JSON
	if str == "" {
		l.ArgError(1, "empty string is not valid JSON")
		return 0
	}

	value, err := Decode(l, []byte(str))
	if err != nil {
		// JSON processing errors - return nil and error
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(value)
	return 1
}

// encode encodes Lua value to JSON string with input validation.
func (*Module) encode(l *lua.LState) int {
	// Input validation errors - use ArgError
	if l.Get(1) == nil {
		l.ArgError(1, "value expected")
		return 0
	}

	value := l.Get(1)
	data, err := Encode(value)
	if err != nil {
		// JSON processing errors - return nil and error
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(data))
	return 1
}

type invalidTypeError lua.LValueType

func (i invalidTypeError) Error() string {
	return `cannot encode ` + lua.LValueType(i).String() + ` to JSON`
}

// Encode returns the JSON encoding of value.
func Encode(value lua.LValue) ([]byte, error) {
	return json.Marshal(jsonValue{
		LValue:  value,
		visited: make(map[*lua.LTable]bool),
	})
}

type jsonValue struct {
	lua.LValue
	visited map[*lua.LTable]bool
}

func (j jsonValue) MarshalJSON() ([]byte, error) {
	switch converted := j.LValue.(type) {
	case lua.LBool:
		return json.Marshal(bool(converted))
	case lua.LNumber:
		return json.Marshal(float64(converted))
	case *lua.LNilType:
		return []byte(`null`), nil
	case lua.LString:
		return json.Marshal(string(converted))
	case *lua.LTable:
		if j.visited[converted] {
			return nil, errNested
		}
		j.visited[converted] = true

		key, value := converted.Next(lua.LNil)

		switch key.Type() {
		case lua.LTNil:
			return []byte(`[]`), nil
		case lua.LTNumber:
			arr := make([]jsonValue, 0, converted.Len())
			expectedKey := lua.LNumber(1)
			for key != lua.LNil {
				if key.Type() != lua.LTNumber {
					return nil, errInvalidKeys
				}
				if expectedKey != key {
					return nil, errSparseArray
				}
				arr = append(arr, jsonValue{value, j.visited})
				expectedKey++
				key, value = converted.Next(key)
			}
			return json.Marshal(arr)
		case lua.LTString:
			obj := make(map[string]jsonValue)
			for key != lua.LNil {
				if key.Type() != lua.LTString {
					return nil, errInvalidKeys
				}
				obj[key.String()] = jsonValue{value, j.visited}
				key, value = converted.Next(key)
			}
			return json.Marshal(obj)
		default:
			return nil, errInvalidKeys
		}
	default:
		return nil, invalidTypeError(j.LValue.Type())
	}
}

// Decode converts JSON encoded data to Lua values.
func Decode(l *lua.LState, data []byte) (lua.LValue, error) {
	var value any
	err := json.Unmarshal(data, &value)
	if err != nil {
		return nil, err
	}
	return DecodeValue(l, value), nil
}

// DecodeValue converts Go value to Lua value.
func DecodeValue(l *lua.LState, value any) lua.LValue {
	switch converted := value.(type) {
	case bool:
		return lua.LBool(converted)
	case float64:
		return lua.LNumber(converted)
	case string:
		return lua.LString(converted)
	case json.Number:
		return lua.LString(converted)
	case []any:
		arr := l.CreateTable(len(converted), 0)
		for _, item := range converted {
			arr.Append(DecodeValue(l, item))
		}
		return arr
	case map[string]any:
		tbl := l.CreateTable(0, len(converted))
		for key, item := range converted {
			tbl.RawSetH(lua.LString(key), DecodeValue(l, item))
		}
		return tbl
	case nil:
		return lua.LNil
	}
	return lua.LNil
}
