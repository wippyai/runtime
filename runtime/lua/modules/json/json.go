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
		t := &lua.LTable{
			Metatable: lua.LNil,
			Immutable: false,
			Strdict: map[string]lua.LValue{
				"decode": l.NewFunction(m.decode),
				"encode": l.NewFunction(m.encode),
			},
		}
		t.Immutable = true
		m.moduleTable = t
	})
	l.Push(m.moduleTable)
	return 1
}

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
		return json.Marshal(float64(converted))
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

func (j *jsonValue) marshalTable(table *lua.LTable) ([]byte, error) {
	if j.visited[table] {
		return nil, errNested
	}
	j.visited[table] = true
	defer delete(j.visited, table)

	// Scan all table parts and collect info in one pass
	var maxArrayIndex lua.LNumber = 0
	arrayCount := 0
	hasStringKeys := false
	totalElements := 0

	// Scan Array part
	if table.Array != nil {
		for i, value := range table.Array {
			if value != lua.LNil {
				totalElements++
				arrayCount++
				idx := lua.LNumber(i + 1) // Convert to 1-indexed
				if idx > maxArrayIndex {
					maxArrayIndex = idx
				}
			}
		}
	}

	// Scan Strdict part
	if table.Strdict != nil {
		for _, value := range table.Strdict {
			if value != lua.LNil {
				totalElements++
				hasStringKeys = true
			}
		}
	}

	// Scan Dict part
	if table.Dict != nil {
		for key, value := range table.Dict {
			if value != lua.LNil {
				totalElements++

				// Check if numeric key like old code
				if num, ok := key.(lua.LNumber); ok {
					if float64(num) == math.Floor(float64(num)) && num > 0 {
						arrayCount++
						if num > maxArrayIndex {
							maxArrayIndex = num
						}
					} else {
						hasStringKeys = true
					}
				} else {
					hasStringKeys = true
				}
			}
		}
	}

	// Empty table check - default to [] like old code
	if totalElements == 0 {
		// Return {} only if dicts are initialized but empty
		if table.Strdict != nil || table.Dict != nil {
			return []byte("{}"), nil
		}
		return []byte("[]"), nil
	}

	// Determine encoding: array if we have numeric keys and either no string keys or treating mixed as objects
	isArray := arrayCount > 0 && (!hasStringKeys || j.options.TreatMixedKeysAsObjects)

	if isArray && hasStringKeys && !j.options.TreatMixedKeysAsObjects {
		return nil, fmt.Errorf("%w: table has both numeric and string keys", errInvalidKeys)
	}

	if isArray {
		return j.encodeAsArray(table, int(maxArrayIndex))
	}

	return j.encodeAsObject(table)
}

func (j *jsonValue) encodeAsArray(table *lua.LTable, maxIndex int) ([]byte, error) {
	if maxIndex == 0 {
		return []byte("[]"), nil
	}

	// Count actual elements
	elementCount := 0
	for i := 1; i <= maxIndex; i++ {
		if j.getValue(table, i) != lua.LNil {
			elementCount++
		}
	}

	// Sparse check
	if elementCount != maxIndex && !j.options.AllowSparseArrays {
		return nil, fmt.Errorf("%w: table has %d logical elements but only %d non-nil elements",
			errSparseArray, maxIndex, elementCount)
	}

	// Build array
	arr := make([]*jsonValue, maxIndex)
	for i := 1; i <= maxIndex; i++ {
		value := j.getValue(table, i)
		arr[i-1] = getJSONValue(value, j.visited, j.depth+1, j.options)
	}

	b, err := json.Marshal(arr)
	for _, child := range arr {
		putJSONValue(child)
	}
	return b, err
}

func (j *jsonValue) encodeAsObject(table *lua.LTable) ([]byte, error) {
	obj := make(map[string]*jsonValue)

	// Process Array part
	if table.Array != nil {
		for i, value := range table.Array {
			if value != lua.LNil {
				key := fmt.Sprintf("%d", i+1)
				obj[key] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	// Process Strdict part
	if table.Strdict != nil {
		for key, value := range table.Strdict {
			if value != lua.LNil {
				obj[key] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	// Process Dict part
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
					continue
				}
				obj[keyStr] = getJSONValue(value, j.visited, j.depth+1, j.options)
			}
		}
	}

	b, err := json.Marshal(obj)
	for _, child := range obj {
		putJSONValue(child)
	}
	return b, err
}

// getValue gets a value at 1-indexed position from any table part
func (j *jsonValue) getValue(table *lua.LTable, index int) lua.LValue {
	// Check Array first
	arrayIndex := index - 1
	if table.Array != nil && arrayIndex >= 0 && arrayIndex < len(table.Array) {
		if value := table.Array[arrayIndex]; value != lua.LNil {
			return value
		}
	}

	// Check Dict for numeric key
	if table.Dict != nil {
		if value, exists := table.Dict[lua.LNumber(index)]; exists && value != lua.LNil {
			return value
		}
	}

	return lua.LNil
}

func Decode(l *lua.LState, data []byte) (lua.LValue, error) {
	var value any
	err := json.Unmarshal(data, &value)
	if err != nil {
		return nil, err
	}
	return DecodeValue(l, value), nil
}

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
		arr := make([]lua.LValue, len(converted))
		for i, item := range converted {
			arr[i] = DecodeValue(l, item)
		}
		return &lua.LTable{
			Metatable: lua.LNil,
			Immutable: false,
			Array:     arr,
		}
	case map[string]any:
		strdict := make(map[string]lua.LValue, len(converted))
		for key, item := range converted {
			strdict[key] = DecodeValue(l, item)
		}
		return &lua.LTable{
			Metatable: lua.LNil,
			Immutable: false,
			Strdict:   strdict,
		}
	case nil:
		return lua.LNil
	}
	return lua.LNil
}
