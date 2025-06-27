package json

import (
	"encoding/json"
	"errors"
	"fmt"
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

var visitedPool = sync.Pool{
	New: func() any {
		return make(map[*lua.LTable]bool)
	},
}

func getJSONValue(lv lua.LValue, visited map[*lua.LTable]bool, depth int, options *EncodeOptions) *jsonValue {
	jv := jsonValuePool.Get().(*jsonValue)
	jv.LValue = lv
	jv.visited = visited
	jv.depth = depth
	jv.options = options
	return jv
}

func putJSONValue(jv *jsonValue) {
	jv.LValue = nil
	jv.visited = nil
	jv.depth = 0
	jv.options = nil
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

	visited := getVisitedMap()
	defer putVisitedMap(visited)

	jv := getJSONValue(value, visited, 0, options)
	b, err := json.Marshal(jv)
	putJSONValue(jv)
	return b, err
}

type jsonValue struct {
	LValue  lua.LValue
	visited map[*lua.LTable]bool // Visited tables for circular reference detection
	depth   int                  // Current nesting depth
	options *EncodeOptions       // Encoding options
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
		if j.visited[converted] {
			return nil, errNested
		}

		// Mark as visited
		j.visited[converted] = true
		defer delete(j.visited, converted)

		// Determine table structure using direct field access
		hasArray := converted.Array != nil && len(converted.Array) > 0
		hasStrdict := converted.Strdict != nil && len(converted.Strdict) > 0
		hasDict := converted.Dict != nil && len(converted.Dict) > 0

		// Check for empty table - distinguish between [] and {}
		if !hasArray && !hasStrdict && !hasDict {
			// If Array field exists but is empty, it's an empty array, default fallback to object
			if converted.Array != nil || converted.Dict == nil {
				return []byte("[]"), nil
			}

			// Otherwise it's an empty object
			return []byte("{}"), nil
		}

		// Check for mixed keys
		isMixed := hasArray && (hasStrdict || hasDict)
		if isMixed && !j.options.TreatMixedKeysAsObjects {
			return nil, fmt.Errorf("%w: table has both numeric and string keys", errInvalidKeys)
		}

		// If it's a pure array or we're treating mixed as object and array is present
		if hasArray && (!hasStrdict && !hasDict || j.options.TreatMixedKeysAsObjects) {
			return j.encodeAsArray(converted)
		}

		// Encode as object
		return j.encodeAsObject(converted)

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

func (j *jsonValue) encodeAsArray(table *lua.LTable) ([]byte, error) {
	arrayLen := len(table.Array)

	// Check for sparse array
	actualLen := table.Len()
	if actualLen != arrayLen && !j.options.AllowSparseArrays {
		return nil, fmt.Errorf("%w: table has %d elements but array length is %d",
			errSparseArray, actualLen, arrayLen)
	}

	// Pre-allocate array for JSON values with exact size
	arr := make([]*jsonValue, arrayLen)

	// Iterate directly over the array
	for i, value := range table.Array {
		arr[i] = getJSONValue(value, j.visited, j.depth+1, j.options)
	}

	b, err := json.Marshal(arr)

	// Clean up
	for _, child := range arr {
		putJSONValue(child)
	}

	return b, err
}

func (j *jsonValue) encodeAsObject(table *lua.LTable) ([]byte, error) {
	// Pre-calculate total size to avoid map resizes
	totalSize := 0
	if table.Array != nil {
		for _, value := range table.Array {
			if value != lua.LNil {
				totalSize++
			}
		}
	}
	if table.Strdict != nil {
		for _, value := range table.Strdict {
			if value != lua.LNil {
				totalSize++
			}
		}
	}
	if table.Dict != nil {
		for _, value := range table.Dict {
			if value != lua.LNil {
				totalSize++
			}
		}
	}

	// Pre-allocate object map with exact size
	obj := make(map[string]*jsonValue, totalSize)

	// Process array part if present (for mixed tables)
	if table.Array != nil {
		for i, value := range table.Array {
			if value != lua.LNil {
				key := fmt.Sprintf("%d", i+1) // Lua is 1-indexed
				obj[key] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	// Process string dictionary
	if table.Strdict != nil {
		for key, value := range table.Strdict {
			if value != lua.LNil {
				obj[key] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	// Process general dictionary
	if table.Dict != nil {
		for key, value := range table.Dict {
			if value != lua.LNil {
				var keyStr string
				switch kt := key.(type) {
				case lua.LString:
					keyStr = string(kt)
				case lua.LNumber:
					keyStr = fmt.Sprintf("%v", float64(kt))
				case lua.LBool:
					keyStr = fmt.Sprintf("%v", bool(kt))
				default:
					// Skip keys that can't be converted to strings
					continue
				}
				obj[keyStr] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	b, err := json.Marshal(obj)

	// Clean up
	for _, child := range obj {
		putJSONValue(child)
	}

	return b, err
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

// DecodeValue converts Go value to Lua value using direct field access.
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
		// Create table and directly populate Array field
		arr := &lua.LTable{
			Metatable: lua.LNil,
			Immutable: false,
			Array:     make([]lua.LValue, len(converted)),
		}
		for i, item := range converted {
			arr.Array[i] = DecodeValue(l, item)
		}
		return arr
	case map[string]any:
		// Create table and directly populate Strdict field
		tbl := &lua.LTable{
			Metatable: lua.LNil,
			Immutable: false,
			Strdict:   make(map[string]lua.LValue, len(converted)),
		}
		for key, item := range converted {
			tbl.Strdict[key] = DecodeValue(l, item)
		}
		return tbl
	case nil:
		return lua.LNil
	}
	return lua.LNil
}
