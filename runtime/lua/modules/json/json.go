// SPDX-License-Identifier: MPL-2.0

package json

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"sync"

	"github.com/kaptinlin/jsonschema"
	lua "github.com/wippyai/go-lua"
	lru "github.com/wippyai/runtime/internal/cache"
	luavalue "github.com/wippyai/runtime/runtime/lua/engine/value"
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
	*jv = jsonValue{lv, visited, options, depth}
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

var (
	globalSchemaCache *lru.Cache[string, *jsonschema.Schema]
	schemaCacheOnce   sync.Once
)

const defaultSchemaCacheSize = 100

func initSchemaCache() {
	schemaCacheOnce.Do(func() {
		globalSchemaCache = lru.New[string, *jsonschema.Schema](lru.WithCapacity(defaultSchemaCacheSize))
	})
}

func validationError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LFalse)
	l.Push(err)
	return 2
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
	options *EncodeOptions
	depth   int
}

func (j *jsonValue) MarshalJSON() ([]byte, error) {
	if j.depth > j.options.MaxDepth {
		return nil, errMaxDepth
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
	case lua.LInteger:
		return json.Marshal(int64(converted))
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
	case *lua.Error:
		return json.Marshal(converted.Error())
	case error:
		return json.Marshal(converted.Error())
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
			return nil, errSparseArray
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
	case lua.LInteger:
		buf.WriteString(strconv.FormatInt(int64(v), 10))
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

// Decode and DecodeValue are defined in decode_v1.go or decode_v2.go
// depending on build tags (goexperiment.jsonv2)

func validationInputError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LFalse)
	l.Push(err)
	return 2
}

func schemaValidateFunc(l *lua.LState) int {
	schemaArg := l.Get(1)
	dataArg := l.Get(2)

	if schemaArg == lua.LNil {
		return validationInputError(l, "schema is required")
	}

	if dataArg == lua.LNil {
		return validationInputError(l, "data is required")
	}

	schemaJSON, err := getSchemaJSON(schemaArg)
	if err != nil {
		return validationError(l, err, "schema error")
	}

	schema, err := compileSchema(schemaJSON)
	if err != nil {
		return validationError(l, err, "compile schema")
	}

	// Convert Lua value directly to Go value
	dataGo := luavalue.ToGoAny(dataArg)

	// Validate using the Go value directly
	var result *jsonschema.EvaluationResult
	if dataMap, ok := dataGo.(map[string]any); ok {
		result = schema.ValidateMap(dataMap)
	} else {
		dataJSON, err := Encode(dataArg)
		if err != nil {
			return validationError(l, err, "convert data")
		}
		result = schema.Validate(dataJSON)
	}

	if !result.IsValid() {
		return validationError(l, result, "validation failed")
	}

	l.Push(lua.LTrue)
	return 1
}

func schemaValidateStringFunc(l *lua.LState) int {
	schemaArg := l.Get(1)
	jsonStr, ok := l.Get(2).(lua.LString)

	if schemaArg == lua.LNil {
		return validationInputError(l, "schema is required")
	}

	if !ok {
		return validationInputError(l, "data must be a JSON string")
	}

	schemaJSON, err := getSchemaJSON(schemaArg)
	if err != nil {
		return validationError(l, err, "schema error")
	}

	schema, err := compileSchema(schemaJSON)
	if err != nil {
		return validationError(l, err, "compile schema")
	}

	result := schema.Validate([]byte(jsonStr))
	if !result.IsValid() {
		return validationError(l, result, "validation failed")
	}

	l.Push(lua.LTrue)
	return 1
}

func getSchemaJSON(schemaArg lua.LValue) ([]byte, error) {
	switch v := schemaArg.(type) {
	case lua.LString:
		return []byte(v), nil
	case *lua.LTable:
		return Encode(v)
	default:
		return nil, errors.New("schema must be a string or table")
	}
}

func compileSchema(schemaJSON []byte) (*jsonschema.Schema, error) {
	cacheKey := hashSchemaJSON(schemaJSON)
	if schema, ok := globalSchemaCache.Get(cacheKey); ok {
		return schema, nil
	}

	compiler := jsonschema.NewCompiler()
	schema, err := compiler.Compile(schemaJSON)
	if err != nil {
		return nil, err
	}

	_ = globalSchemaCache.Set(cacheKey, schema)
	return schema, nil
}

func hashSchemaJSON(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
