package json

import (
	"encoding/json"
	"errors"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

var (
	errNested      = errors.New("cannot encode recursively nested tables to JSON")
	errSparseArray = errors.New("cannot encode sparse array")
	errInvalidKeys = errors.New("cannot encode mixed or invalid key types")
)

var jsonValuePool = sync.Pool{
	New: func() any {
		return &jsonValue{}
	},
}

func getJSONValue(lv lua.LValue, visited map[*lua.LTable]bool) *jsonValue {
	jv := jsonValuePool.Get().(*jsonValue)
	jv.LValue = lv
	jv.visited = visited
	return jv
}

func putJSONValue(jv *jsonValue) {
	jv.LValue = nil
	jv.visited = nil
	jsonValuePool.Put(jv)
}

// Module represents JSON bindings to Lua VM.
type Module struct{}

// NewJSONModule creates a new JSON module.
func NewJSONModule() *Module {
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
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.ToString(1)
	if str == "" {
		l.ArgError(1, "empty string is not valid JSON")
		return 0
	}

	value, err := Decode(l, []byte(str))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(value)
	return 1
}

// encode encodes Lua value to JSON string with input validation.
func (*Module) encode(l *lua.LState) int {
	if l.Get(1) == nil {
		l.ArgError(1, "value expected")
		return 0
	}

	value := l.Get(1)
	data, err := Encode(value)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(data))
	return 1
}

type invalidTypeError lua.LValueType

func (i invalidTypeError) Error() string {
	return "cannot encode " + lua.LValueType(i).String() + " to JSON"
}

// Encode returns the JSON encoding of value.
func Encode(value lua.LValue) ([]byte, error) {
	visited := make(map[*lua.LTable]bool)
	jv := getJSONValue(value, visited)
	b, err := json.Marshal(jv)
	putJSONValue(jv)
	return b, err
}

type jsonValue struct {
	LValue  lua.LValue
	visited map[*lua.LTable]bool
}

func (j *jsonValue) MarshalJSON() ([]byte, error) {
	switch converted := j.LValue.(type) {
	case lua.LBool:
		return json.Marshal(bool(converted))
	case lua.LNumber:
		return json.Marshal(float64(converted))
	case *lua.LNilType:
		return []byte("null"), nil
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
			return []byte("[]"), nil
		case lua.LTNumber:
			arr := make([]*jsonValue, 0, converted.Len())
			expectedKey := lua.LNumber(1)
			for key != lua.LNil {
				if key.Type() != lua.LTNumber {
					return nil, errInvalidKeys
				}
				if expectedKey != key {
					return nil, errSparseArray
				}
				child := getJSONValue(value, j.visited)
				arr = append(arr, child)
				expectedKey++
				key, value = converted.Next(key)
			}
			b, err := json.Marshal(arr)
			for _, child := range arr {
				putJSONValue(child)
			}
			return b, err
		case lua.LTString:
			obj := make(map[string]*jsonValue, converted.Len())
			for key != lua.LNil {
				if key.Type() != lua.LTString {
					return nil, errInvalidKeys
				}
				child := getJSONValue(value, j.visited)
				obj[key.String()] = child
				key, value = converted.Next(key)
			}
			b, err := json.Marshal(obj)
			for _, child := range obj {
				putJSONValue(child)
			}
			return b, err
		case lua.LTBool, lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTTable, lua.LTChannel:
			fallthrough
		default:
			return nil, errInvalidKeys
		}
	case *lua.LUserData:
		if str, ok := converted.Value.(string); ok {
			return json.Marshal(str)
		}

		if err, ok := converted.Value.(error); ok {
			return json.Marshal(err.Error())
		}

		return nil, invalidTypeError(j.LValue.Type())
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
