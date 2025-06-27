package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

var (
	errNested      = errors.New("cannot encode recursively nested tables to JSON")
	errSparseArray = errors.New("cannot encode sparse array: non-contiguous numeric keys found")
	errInvalidKeys = errors.New("cannot encode mixed-key table: table has both numeric and non-numeric keys")
	errMaxDepth    = errors.New("exceeded maximum nesting depth for JSON encoding")
)

const DefaultMaxDepth = 128

type EncodeOptions struct {
	MaxDepth                int
	AllowSparseArrays       bool
	TreatMixedKeysAsObjects bool
}

var DefaultEncodeOptions = EncodeOptions{
	MaxDepth:                DefaultMaxDepth,
	AllowSparseArrays:       false,
	TreatMixedKeysAsObjects: false,
}

var (
	jsonValuePool = sync.Pool{
		New: func() any { return &jsonValue{} },
	}
	visitedPool = sync.Pool{
		New: func() any { return make(map[*lua.LTable]bool, 16) },
	}
)

func getJSONValue(lv lua.LValue, visited map[*lua.LTable]bool, depth int, options *EncodeOptions) *jsonValue {
	jv := jsonValuePool.Get().(*jsonValue)
	*jv = jsonValue{lv, visited, depth, options}
	return jv
}

func putJSONValue(jv *jsonValue) {
	*jv = jsonValue{}
	jsonValuePool.Put(jv)
}

func getVisitedMap() map[*lua.LTable]bool {
	return visitedPool.Get().(map[*lua.LTable]bool)
}

func putVisitedMap(visited map[*lua.LTable]bool) {
	for k := range visited {
		delete(visited, k)
	}
	visitedPool.Put(visited)
}

type Module struct {
	Options     EncodeOptions
	moduleTable *lua.LTable
	once        sync.Once
}

func NewJSONModule() *Module {
	return &Module{Options: DefaultEncodeOptions}
}

func (m *Module) Name() string {
	return "json"
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		t := l.NewTable()
		l.SetField(t, "decode", l.NewFunction(m.decode))
		l.SetField(t, "encode", l.NewFunction(m.encode))
		m.moduleTable = t
	})
	l.Push(m.moduleTable)
	return 1
}

func (m *Module) decode(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	if str == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty string is not valid JSON"))
		return 2
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

func (m *Module) encode(l *lua.LState) int {
	value := l.Get(1)
	if value == lua.LNil {
		l.Push(lua.LString("null"))
		return 1
	}

	data, err := EncodeWithOptions(value, &m.Options)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(data))
	return 1
}

func Encode(value lua.LValue) ([]byte, error) {
	return EncodeWithOptions(value, &DefaultEncodeOptions)
}

func EncodeWithOptions(value lua.LValue, options *EncodeOptions) ([]byte, error) {
	if options == nil {
		options = &DefaultEncodeOptions
	}
	if options.MaxDepth <= 0 {
		options.MaxDepth = DefaultMaxDepth
	}

	visited := getVisitedMap()
	defer putVisitedMap(visited)

	jv := getJSONValue(value, visited, 0, options)
	b, err := json.Marshal(jv)
	putJSONValue(jv)
	return b, err
}

type jsonValue struct {
	LValue  lua.LValue
	visited map[*lua.LTable]bool
	depth   int
	options *EncodeOptions
}

func (j *jsonValue) MarshalJSON() ([]byte, error) {
	if j.depth > j.options.MaxDepth {
		return nil, fmt.Errorf("%w: exceeded maximum depth of %d", errMaxDepth, j.options.MaxDepth)
	}

	switch converted := j.LValue.(type) {
	case lua.LBool:
		if converted {
			return []byte("true"), nil
		}
		return []byte("false"), nil
	case lua.LNumber:
		f := float64(converted)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return []byte("null"), nil
		}
		return json.Marshal(f)
	case *lua.LNilType:
		return []byte("null"), nil
	case lua.LString:
		return json.Marshal(string(converted))
	case *lua.LTable:
		return j.marshalTable(converted)
	case *lua.LUserData:
		if str, ok := converted.Value.(string); ok {
			return json.Marshal(str)
		}
		if err, ok := converted.Value.(error); ok {
			return json.Marshal(err.Error())
		}
		return []byte("null"), nil
	default:
		return []byte("null"), nil
	}
}

func isInteger(n lua.LNumber) bool {
	return float64(n) == math.Floor(float64(n))
}

func (j *jsonValue) marshalTable(table *lua.LTable) ([]byte, error) {
	if j.visited[table] {
		return nil, errNested
	}
	j.visited[table] = true
	defer delete(j.visited, table)

	var arrayPart map[int]lua.LValue
	var objectPart map[string]*jsonValue
	maxNumericKey := 0
	isObject := false
	elementCount := 0

	if table.Array != nil {
		for i, value := range table.Array {
			if value != lua.LNil {
				if arrayPart == nil {
					arrayPart = make(map[int]lua.LValue)
				}
				idx := i + 1
				arrayPart[idx] = value
				if idx > maxNumericKey {
					maxNumericKey = idx
				}
				elementCount++
			}
		}
	}

	if table.Strdict != nil {
		for key, value := range table.Strdict {
			if value != lua.LNil {
				if !isObject {
					isObject = true
					objectPart = make(map[string]*jsonValue)
				}
				objectPart[key] = getJSONValue(value, j.visited, j.depth+1, j.options)
				elementCount++
			}
		}
	}

	if table.Dict != nil {
		for key, value := range table.Dict {
			if value != lua.LNil {
				var keyStr string
				isNumericKey := false

				if num, ok := key.(lua.LNumber); ok {
					if isInteger(num) && num > 0 {
						idx := int(num)
						if arrayPart == nil {
							arrayPart = make(map[int]lua.LValue)
						}
						arrayPart[idx] = value
						if idx > maxNumericKey {
							maxNumericKey = idx
						}
						isNumericKey = true
					} else {
						keyStr = strconv.FormatFloat(float64(num), 'f', -1, 64)
					}
				} else if s, ok := key.(lua.LString); ok {
					keyStr = string(s)
				} else if b, ok := key.(lua.LBool); ok {
					keyStr = strconv.FormatBool(bool(b))
				} else {
					continue
				}

				if !isNumericKey {
					if !isObject {
						isObject = true
						objectPart = make(map[string]*jsonValue)
					}
					objectPart[keyStr] = getJSONValue(value, j.visited, j.depth+1, j.options)
				}
				elementCount++
			}
		}
	}

	if elementCount == 0 {
		if table.Strdict != nil || table.Dict != nil {
			return []byte("{}"), nil
		}
		return []byte("[]"), nil
	}

	if isObject {
		if len(arrayPart) > 0 && !j.options.TreatMixedKeysAsObjects {
			for _, child := range objectPart {
				putJSONValue(child)
			}
			return nil, errInvalidKeys
		}
		for idx, value := range arrayPart {
			objectPart[strconv.Itoa(idx)] = getJSONValue(value, j.visited, j.depth+1, j.options)
		}
	} else {
		if len(arrayPart) != maxNumericKey && !j.options.AllowSparseArrays {
			return nil, fmt.Errorf("%w: max key is %d but only %d elements found", errSparseArray, maxNumericKey, len(arrayPart))
		}
	}

	if isObject {
		b, err := json.Marshal(objectPart)
		for _, child := range objectPart {
			putJSONValue(child)
		}
		return b, err
	}

	if maxNumericKey == 0 {
		return []byte("[]"), nil
	}
	arr := make([]*jsonValue, maxNumericKey)
	for i := 1; i <= maxNumericKey; i++ {
		value, ok := arrayPart[i]
		if !ok {
			value = lua.LNil
		}
		arr[i-1] = getJSONValue(value, j.visited, j.depth+1, j.options)
	}
	b, err := json.Marshal(arr)
	for _, child := range arr {
		putJSONValue(child)
	}
	return b, err
}

func Decode(l *lua.LState, data []byte) (lua.LValue, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return DecodeValue(l, value), nil
}

// DecodeValue converts Go value to Lua value with proper indexing.
func DecodeValue(l *lua.LState, value any) lua.LValue {
	switch converted := value.(type) {
	case bool:
		return lua.LBool(converted)
	case json.Number:
		if f, err := converted.Float64(); err == nil {
			return lua.LNumber(f)
		}
		return lua.LString(converted.String())
	case string:
		return lua.LString(converted)
	case []any:
		// Use proper Lua table creation with 1-indexed arrays
		arr := l.CreateTable(len(converted), 0)
		for i, item := range converted {
			arr.RawSetInt(i+1, DecodeValue(l, item)) // 1-indexed for Lua
		}
		return arr
	case map[string]any:
		// Use proper Lua table creation for objects
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
