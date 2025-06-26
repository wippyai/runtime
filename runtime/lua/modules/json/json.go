package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

var (
	errNested      = errors.New("cannot encode recursively nested tables to JSON")
	errSparseArray = errors.New("cannot encode sparse array")
	errInvalidKeys = errors.New("cannot encode mixed or invalid key types")
	errMaxDepth    = errors.New("exceeded maximum nesting depth for JSON encoding")
)

// DefaultMaxDepth is the default maximum nesting depth for JSON encoding
const DefaultMaxDepth = 128

// EncodeOptions controls the behavior of the JSON encoder
type EncodeOptions struct {
	// MaxDepth is the maximum nesting depth for JSON encoding
	MaxDepth int
	// AllowSparseArrays allows sparse arrays to be encoded by filling gaps with null
	AllowSparseArrays bool
	// TreatMixedKeysAsObjects converts tables with mixed key types to objects
	TreatMixedKeysAsObjects bool
}

// DefaultEncodeOptions provides sensible defaults for JSON encoding
//

var DefaultEncodeOptions = EncodeOptions{
	MaxDepth:                DefaultMaxDepth,
	AllowSparseArrays:       false,
	TreatMixedKeysAsObjects: false,
}

var jsonValuePool = sync.Pool{
	New: func() any {
		return &jsonValue{}
	},
}

func getJSONValue(lv lua.LValue, path []*lua.LTable, depth int, options *EncodeOptions) *jsonValue {
	jv := jsonValuePool.Get().(*jsonValue)
	jv.LValue = lv
	jv.path = path
	jv.depth = depth
	jv.options = options
	return jv
}

func putJSONValue(jv *jsonValue) {
	jv.LValue = nil
	jv.path = nil
	jv.depth = 0
	jv.options = nil
	jsonValuePool.Put(jv)
}

// Module represents JSON bindings to Lua VM.
type Module struct {
	Options EncodeOptions // Options for controlling encoding behavior
}

// NewJSONModule creates a new JSON module with default options.
func NewJSONModule() *Module {
	return &Module{
		Options: DefaultEncodeOptions,
	}
}

// Name returns the module name.
func (m *Module) Name() string {
	return "json"
}

// Loader registers the module's functions into Lua state.
func (m *Module) Loader(l *lua.LState) int {
	t := l.CreateTable(0, 2)

	t.RawSetString("decode", l.NewFunction(m.decode))
	t.RawSetString("encode", l.NewFunction(m.encode))

	l.Push(t)
	return 1
}

// decode decodes JSON string to Lua value with input validation.
func (*Module) decode(l *lua.LState) int {
	if l.Get(1).Type() != lua.LTString {
		l.Push(lua.LNil)
		l.Push(lua.LString("string expected"))
		return 2
	}

	str := l.ToString(1)
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

// encode encodes Lua value to JSON string with input validation.
func (m *Module) encode(l *lua.LState) int {
	value := l.Get(1)
	if value == lua.LNil {
		l.Push(lua.LString("null"))
		return 1
	}

	opts := m.Options
	data, err := EncodeWithOptions(value, &opts)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LString(data))
	return 1
}

// Encode returns the JSON encoding of value with default options.
func Encode(value lua.LValue) ([]byte, error) {
	return EncodeWithOptions(value, &DefaultEncodeOptions)
}

// EncodeWithOptions returns the JSON encoding of value with specified options.
func EncodeWithOptions(value lua.LValue, options *EncodeOptions) ([]byte, error) {
	if options == nil {
		options = &DefaultEncodeOptions
	}

	if options.MaxDepth <= 0 {
		options.MaxDepth = DefaultMaxDepth
	}

	// Empty initial path
	var path []*lua.LTable
	jv := getJSONValue(value, path, 0, options)
	b, err := json.Marshal(jv)
	putJSONValue(jv)
	return b, err
}

type jsonValue struct {
	LValue  lua.LValue
	path    []*lua.LTable  // Current path of parent tables
	depth   int            // Current nesting depth
	options *EncodeOptions // Encoding options
}

// tableInPath checks if the table already exists in the current path
func tableInPath(path []*lua.LTable, table *lua.LTable) bool {
	for _, t := range path {
		if t == table {
			return true
		}
	}
	return false
}

// appendTableToPath creates a new path with the table appended
func appendTableToPath(path []*lua.LTable, table *lua.LTable) []*lua.LTable {
	newPath := make([]*lua.LTable, len(path)+1)
	copy(newPath, path)
	newPath[len(path)] = table
	return newPath
}

func (j *jsonValue) MarshalJSON() ([]byte, error) {
	// Check depth limit
	if j.depth > j.options.MaxDepth {
		return nil, fmt.Errorf("%w: exceeded maximum depth of %d", errMaxDepth, j.options.MaxDepth)
	}

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
		// Check for circular reference
		if tableInPath(j.path, converted) {
			return nil, errNested
		}

		// Create new path with this table for child nodes
		newPath := appendTableToPath(j.path, converted)

		// First, determine if this table is empty
		if key, _ := converted.Next(lua.LNil); key == lua.LNil {
			return []byte("[]"), nil // todo: patch Table
		}

		// Now, analyze the table to determine how to encode it
		isArray, isObject, maxArrayIndex, err := analyzeTable(converted)
		if err != nil && !j.options.TreatMixedKeysAsObjects {
			return nil, err
		}

		// If table is an array (sequential numeric keys)
		if isArray && (!isObject || j.options.TreatMixedKeysAsObjects) {
			// Create an array to hold all values
			arr := make([]*jsonValue, int(maxArrayIndex))

			// If we have a sparse array and it's not allowed, return an error
			if int(maxArrayIndex) != converted.Len() && !j.options.AllowSparseArrays {
				return nil, fmt.Errorf("%w: table has %d elements but max index is %d",
					errSparseArray, converted.Len(), int(maxArrayIndex))
			}

			// Fill the array with values or nil for gaps
			var idx int
			for i := 1; i <= int(maxArrayIndex); i++ {
				lv := converted.RawGetInt(i)
				if lv == lua.LNil && j.options.AllowSparseArrays {
					// Fill gap with nil for sparse arrays
					arr[idx] = getJSONValue(lua.LNil, newPath, j.depth+1, j.options)
				} else {
					arr[idx] = getJSONValue(lv, newPath, j.depth+1, j.options)
				}
				idx++
			}

			b, err := json.Marshal(arr)
			for _, child := range arr {
				putJSONValue(child)
			}
			return b, err
		}

		// It's an object (string keys) or we're treating mixed keys as object
		obj := make(map[string]*jsonValue)

		// Iterate through all keys
		for k, v := converted.Next(lua.LNil); k != lua.LNil; k, v = converted.Next(k) {
			var keyStr string

			// Convert the key to string
			switch kt := k.(type) {
			case lua.LString:
				keyStr = string(kt)
			case lua.LNumber:
				// Convert number keys to strings for objects
				keyStr = fmt.Sprintf("%v", float64(kt))
			case lua.LBool:
				// Convert boolean keys to strings
				keyStr = fmt.Sprintf("%v", bool(kt))
			default:
				// Skip keys that can't be converted to strings
				continue
			}

			obj[keyStr] = getJSONValue(v, newPath, j.depth+1, j.options)
		}

		b, err := json.Marshal(obj)
		for _, child := range obj {
			putJSONValue(child)
		}
		return b, err
	case *lua.LUserData:
		if str, ok := converted.Value.(string); ok {
			return json.Marshal(str)
		}

		if err, ok := converted.Value.(error); ok {
			return json.Marshal(err.Error())
		}

		return []byte("null"), nil
	default:
		// Functions, threads, channels, etc. are encoded as null
		return []byte("null"), nil
	}
}

// analyzeTable determines if a table is an array, object, or mixed,
// and returns the maximum array index if applicable
func analyzeTable(table *lua.LTable) (isArray bool, isObject bool, maxArrayIndex lua.LNumber, err error) {
	maxArrayIndex = 0
	arrayCount := 0

	for k, _ := table.Next(lua.LNil); k != lua.LNil; k, _ = table.Next(k) {
		switch kt := k.(type) {
		case lua.LNumber:
			// Check if it's a valid array index (positive integer)
			if float64(kt) == math.Floor(float64(kt)) && kt > 0 {
				isArray = true
				arrayCount++
				if kt > maxArrayIndex {
					maxArrayIndex = kt
				}
			} else {
				// Non-integer or negative number keys are treated as object keys
				isObject = true
			}
		case lua.LString:
			isObject = true
		default:
			// Other key types (boolean, etc.) are treated as invalid
			return false, false, 0, fmt.Errorf("%w: table has keys of type %s",
				errInvalidKeys, k.Type().String())
		}
	}

	// Check if this looks like an array (sequential numeric keys)
	// FIXME implement
	//nolint:revive,staticcheck // ignore for now
	if isArray && arrayCount != int(maxArrayIndex) {
		// It's a sparse array
		// We'll return isArray=true but the caller needs to check if sparse arrays are allowed
	}

	// If we have both array and object keys, it's mixed
	if isArray && isObject {
		return true, true, maxArrayIndex, fmt.Errorf("%w: table has both numeric and string keys",
			errInvalidKeys)
	}

	return isArray, isObject, maxArrayIndex, nil
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
		for i, item := range converted {
			arr.RawSetInt(i+1, DecodeValue(l, item))
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
