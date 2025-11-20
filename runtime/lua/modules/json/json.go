package json

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/kaptinlin/jsonschema"
	"github.com/wippyai/runtime/internal/cache"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
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
	// Pool for JSON writing buffers
	bufferPool = sync.Pool{
		New: func() any { return &bytes.Buffer{} },
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

func getBuffer() *bytes.Buffer {
	return bufferPool.Get().(*bytes.Buffer)
}

func putBuffer(buf *bytes.Buffer) {
	if buf.Cap() > 64*1024 { // Don't pool huge buffers
		return
	}
	buf.Reset()
	bufferPool.Put(buf)
}

func isSimpleASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] < 0x20 {
			return false
		}
	}
	return true
}

func needsEscaping(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' || c < 0x20 {
			return true
		}
	}
	return false
}

func writeSimpleASCIIString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	buf.WriteString(s)
	buf.WriteByte('"')
}

type Module struct {
	Options     EncodeOptions
	EnableCache bool
	CacheSize   int
	schemaCache *lru.Cache[string, *jsonschema.Schema]
	moduleTable *lua.LTable
	once        sync.Once
}

type Option func(*Module)

func WithCache(enabled bool) Option {
	return func(m *Module) {
		m.EnableCache = enabled
	}
}

func WithCapacity(capacity int) Option {
	return func(m *Module) {
		m.CacheSize = capacity
	}
}

func NewJSONModule(opts ...Option) *Module {
	m := &Module{
		Options:     DefaultEncodeOptions,
		EnableCache: false,
		CacheSize:   100,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Module) Name() string {
	return "json"
}

func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		if m.EnableCache && m.schemaCache == nil {
			m.schemaCache = lru.New[string, *jsonschema.Schema](lru.WithCapacity(m.CacheSize))
		}

		t := l.NewTable()
		t.RawSetString("decode", l.NewFunction(m.decode))
		t.RawSetString("encode", l.NewFunction(m.encode))
		t.RawSetString("validate", l.NewFunction(m.validate))
		t.RawSetString("validate_string", l.NewFunction(m.validateString))
		t.Immutable = true
		m.moduleTable = t
	})
	l.Push(m.moduleTable)
	return 1
}

func (m *Module) decode(l *lua.LState) int {
	str, ok := l.Get(1).(lua.LString)
	if !ok {
		l.Push(lua.LNil)
		l.Push(newJSONInvalidError(l, "string expected", "decode"))
		return 2
	}

	if str == "" {
		l.Push(lua.LNil)
		l.Push(newJSONInvalidError(l, "empty string is not valid JSON", "decode"))
		return 2
	}

	value, err := Decode(l, []byte(str))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(newJSONDecodeError(l, err))
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
		l.Push(newJSONDecodeError(l, err))
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
		return j.marshalTableDirect(converted)
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

// marshalTableDirect writes JSON directly without intermediate Go structures
func (j *jsonValue) marshalTableDirect(table *lua.LTable) ([]byte, error) {
	if j.visited[table] {
		return nil, errNested
	}
	j.visited[table] = true
	defer delete(j.visited, table)

	buf := getBuffer()
	defer putBuffer(buf)

	// Scan to determine structure
	maxNumericKey := 0
	hasStringKeys := false
	hasNumericKeys := false
	elementCount := 0

	// Check Array part
	if table.Array != nil {
		for i, value := range table.Array {
			if value != lua.LNil {
				hasNumericKeys = true
				idx := i + 1
				if idx > maxNumericKey {
					maxNumericKey = idx
				}
				elementCount++
			}
		}
	}

	// Check Strdict part
	if table.Strdict != nil {
		for _, value := range table.Strdict {
			if value != lua.LNil {
				hasStringKeys = true
				elementCount++
			}
		}
	}

	// Check Dict part
	if table.Dict != nil {
		for key, value := range table.Dict {
			if value != lua.LNil {
				if num, ok := key.(lua.LNumber); ok && isInteger(num) && num > 0 {
					hasNumericKeys = true
					idx := int(num)
					if idx > maxNumericKey {
						maxNumericKey = idx
					}
				} else {
					hasStringKeys = true
				}
				elementCount++
			}
		}
	}

	// Handle empty table
	if elementCount == 0 {
		if hasStringKeys || table.Strdict != nil || table.Dict != nil {
			return []byte("{}"), nil
		}
		return []byte("[]"), nil
	}

	// Determine if we should encode as object or array
	isObject := hasStringKeys
	if hasNumericKeys && hasStringKeys && !j.options.TreatMixedKeysAsObjects {
		return nil, errInvalidKeys
	}

	if isObject {
		return j.writeObjectDirect(buf, table, maxNumericKey)
	}

	// Check for sparse array
	if maxNumericKey > 0 && !j.options.AllowSparseArrays {
		actualCount := 0
		if table.Array != nil {
			for _, value := range table.Array {
				if value != lua.LNil {
					actualCount++
				}
			}
		}
		if table.Dict != nil {
			for key, value := range table.Dict {
				if value != lua.LNil {
					if num, ok := key.(lua.LNumber); ok && isInteger(num) && num > 0 {
						actualCount++
					}
				}
			}
		}
		if actualCount != maxNumericKey {
			return nil, fmt.Errorf("%w: max key is %d but only %d elements found", errSparseArray, maxNumericKey, actualCount)
		}
	}

	return j.writeArrayDirect(buf, table, maxNumericKey)
}

func (j *jsonValue) writeArrayDirect(buf *bytes.Buffer, table *lua.LTable, maxNumericKey int) ([]byte, error) {
	if maxNumericKey == 0 {
		return []byte("[]"), nil
	}

	buf.WriteByte('[')
	first := true

	for i := 1; i <= maxNumericKey; i++ {
		var value = lua.LNil

		// Check Array part
		if table.Array != nil && i-1 < len(table.Array) {
			if v := table.Array[i-1]; v != lua.LNil {
				value = v
			}
		}

		// Check Dict part for numeric keys
		if value == lua.LNil && table.Dict != nil {
			if v, ok := table.Dict[lua.LNumber(i)]; ok {
				value = v
			}
		}

		if !first {
			buf.WriteByte(',')
		}
		first = false

		if err := j.writeValueOptimized(buf, value); err != nil {
			return nil, err
		}
	}

	buf.WriteByte(']')
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func (j *jsonValue) writeObjectDirect(buf *bytes.Buffer, table *lua.LTable, maxNumericKey int) ([]byte, error) {
	buf.WriteByte('{')
	first := true

	writeKeyValue := func(key string, value lua.LValue) error {
		if !first {
			buf.WriteByte(',')
		}
		first = false

		//  Fast path for simple ASCII keys
		if isSimpleASCII(key) && !needsEscaping(key) {
			writeSimpleASCIIString(buf, key)
		} else {
			// Fallback to safe marshaling for complex keys
			keyBytes, err := json.Marshal(key)
			if err != nil {
				return err
			}
			buf.Write(keyBytes)
		}

		buf.WriteByte(':')

		return j.writeValueOptimized(buf, value)
	}

	// Write numeric keys first (if treating mixed as objects)
	if maxNumericKey > 0 {
		for i := 1; i <= maxNumericKey; i++ {
			var value = lua.LNil

			// Check Array part
			if table.Array != nil && i-1 < len(table.Array) {
				if v := table.Array[i-1]; v != lua.LNil {
					value = v
				}
			}

			// Check Dict part
			if value == lua.LNil && table.Dict != nil {
				if v, ok := table.Dict[lua.LNumber(i)]; ok {
					value = v
				}
			}

			if value != lua.LNil {
				if err := writeKeyValue(strconv.Itoa(i), value); err != nil {
					return nil, err
				}
			}
		}
	}

	// Write string keys
	if table.Strdict != nil {
		for key, value := range table.Strdict {
			if value != lua.LNil {
				if err := writeKeyValue(key, value); err != nil {
					return nil, err
				}
			}
		}
	}

	// Write non-numeric Dict keys
	if table.Dict != nil {
		for key, value := range table.Dict {
			if value != lua.LNil {
				var keyStr string
				isNumericKey := false

				if num, ok := key.(lua.LNumber); ok {
					if isInteger(num) && num > 0 {
						isNumericKey = true // Skip, already handled above
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
					if err := writeKeyValue(keyStr, value); err != nil {
						return nil, err
					}
				}
			}
		}
	}

	buf.WriteByte('}')
	result := make([]byte, buf.Len())
	copy(result, buf.Bytes())
	return result, nil
}

func (j *jsonValue) writeValueOptimized(buf *bytes.Buffer, value lua.LValue) error {
	switch v := value.(type) {
	case lua.LString:
		str := string(v)
		if isSimpleASCII(str) && !needsEscaping(str) {
			writeSimpleASCIIString(buf, str)
			return nil
		}
		// Fallback to safe marshaling for complex strings
		valBytes, err := json.Marshal(str)
		if err != nil {
			return err
		}
		buf.Write(valBytes)
		return nil
	case lua.LNumber:
		f := float64(v)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			buf.WriteString("null")
		} else {
			// Check if it's an integer to avoid scientific notation
			if f == math.Floor(f) && f >= math.MinInt64 && f <= math.MaxInt64 {
				// Format as integer to avoid scientific notation
				buf.WriteString(strconv.FormatInt(int64(f), 10))
			} else {
				// Format as float
				buf.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
			}
		}
		return nil
	case lua.LBool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case *lua.LNilType:
		buf.WriteString("null")
		return nil
	default:
		// Complex types: use existing safe recursive approach
		childJSON := getJSONValue(value, j.visited, j.depth+1, j.options)
		childBytes, err := childJSON.MarshalJSON()
		putJSONValue(childJSON)
		if err != nil {
			return err
		}
		buf.Write(childBytes)
		return nil
	}
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

func (m *Module) validate(l *lua.LState) int {
	schemaArg := l.Get(1)
	dataArg := l.Get(2)

	if schemaArg == lua.LNil {
		l.Push(lua.LFalse)
		l.Push(newJSONInvalidError(l, "schema is required", "validate"))
		return 2
	}

	if dataArg == lua.LNil {
		l.Push(lua.LFalse)
		l.Push(newJSONInvalidError(l, "data is required", "validate"))
		return 2
	}

	schemaJSON, err := m.getSchemaJSON(l, schemaArg)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, err))
		return 2
	}

	schema, err := m.compileSchema(schemaJSON)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, fmt.Errorf("compile schema: %w", err)))
		return 2
	}

	// Convert Lua value directly to Go value (no JSON encoding needed)
	dataGo := value.ToGoAny(dataArg)

	// Validate using the Go value directly
	var result *jsonschema.EvaluationResult
	if dataMap, ok := dataGo.(map[string]any); ok {
		// For objects, use ValidateMap for better performance
		result = schema.ValidateMap(dataMap)
	} else {
		// For primitives and arrays, encode to JSON and validate
		dataJSON, err := EncodeWithOptions(dataArg, &m.Options)
		if err != nil {
			l.Push(lua.LFalse)
			l.Push(newJSONValidationError(l, fmt.Errorf("convert data: %w", err)))
			return 2
		}
		result = schema.Validate(dataJSON)
	}

	if !result.IsValid() {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, result))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) validateString(l *lua.LState) int {
	schemaArg := l.Get(1)
	jsonStr, ok := l.Get(2).(lua.LString)

	if schemaArg == lua.LNil {
		l.Push(lua.LFalse)
		l.Push(newJSONInvalidError(l, "schema is required", "validate_string"))
		return 2
	}

	if !ok {
		l.Push(lua.LFalse)
		l.Push(newJSONInvalidError(l, "data must be a JSON string", "validate_string"))
		return 2
	}

	schemaJSON, err := m.getSchemaJSON(l, schemaArg)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, err))
		return 2
	}

	schema, err := m.compileSchema(schemaJSON)
	if err != nil {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, fmt.Errorf("compile schema: %w", err)))
		return 2
	}

	result := schema.Validate([]byte(jsonStr))
	if !result.IsValid() {
		l.Push(lua.LFalse)
		l.Push(newJSONValidationError(l, result))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) getSchemaJSON(l *lua.LState, schemaArg lua.LValue) ([]byte, error) {
	switch v := schemaArg.(type) {
	case lua.LString:
		return []byte(v), nil
	case *lua.LTable:
		return EncodeWithOptions(v, &m.Options)
	default:
		return nil, errors.New("schema must be a string or table")
	}
}

func (m *Module) compileSchema(schemaJSON []byte) (*jsonschema.Schema, error) {
	if m.schemaCache == nil {
		compiler := jsonschema.NewCompiler()
		return compiler.Compile(schemaJSON)
	}

	cacheKey := m.hashSchemaJSON(schemaJSON)
	if schema, ok := m.schemaCache.Get(cacheKey); ok {
		return schema, nil
	}

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaJSON)
	if err != nil {
		return nil, err
	}

	m.schemaCache.Set(cacheKey, schema)
	return schema, nil
}

func (m *Module) hashSchemaJSON(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func newJSONValidationError(l *lua.LState, err error) lua.LValue {
	tbl := l.NewTable()
	tbl.RawSetString("message", lua.LString(err.Error()))
	tbl.RawSetString("type", lua.LString("validation_error"))
	return tbl
}
