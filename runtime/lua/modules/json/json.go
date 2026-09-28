// SPDX-License-Identifier: MPL-2.0

package json

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	encodeStatePool = sync.Pool{
		New: func() any {
			return &encodeState{
				visited: make(map[*lua.LTable]bool, 16),
				entries: make([]objectEntry, 0, 64),
			}
		},
	}
	// Pool for JSON writing buffers
	bufferPool = sync.Pool{
		New: func() any { return &bytes.Buffer{} },
	}
)

// encodeState is shared by every value of one Encode call. entries is a stack:
// each object sorts its own hash keys in a segment above its parent's segment.
type encodeState struct {
	visited map[*lua.LTable]bool
	entries []objectEntry
}

// objectEntry is one hash key of a table being written as a JSON object.
// rank orders entries whose written keys are equal: Strdict keys first, then
// Dict string, number and boolean keys.
type objectEntry struct {
	value lua.LValue
	key   string
	rank  uint8
}

const (
	rankStrdict uint8 = iota
	rankDictString
	rankDictNumber
	rankDictBool
)

func compareObjectEntries(a, b objectEntry) int {
	if c := strings.Compare(a.key, b.key); c != 0 {
		return c
	}
	return int(a.rank) - int(b.rank)
}

func objectEntryLess(a, b *objectEntry) bool {
	return a.key < b.key || (a.key == b.key && a.rank < b.rank)
}

// maxInsertionSortEntries is the object width up to which entries are sorted
// by insertion; typical objects are narrow and this avoids a comparator call
// per comparison.
const maxInsertionSortEntries = 12

func sortObjectEntries(entries []objectEntry) {
	if len(entries) > maxInsertionSortEntries {
		slices.SortFunc(entries, compareObjectEntries)
		return
	}
	for i := 1; i < len(entries); i++ {
		entry := entries[i]
		j := i
		for j > 0 && objectEntryLess(&entry, &entries[j-1]) {
			entries[j] = entries[j-1]
			j--
		}
		entries[j] = entry
	}
}

func getJSONValue(lv lua.LValue, state *encodeState, depth int, options *EncodeOptions) *jsonValue {
	jv := jsonValuePool.Get().(*jsonValue)
	*jv = jsonValue{lv, state, options, depth}
	return jv
}

func putJSONValue(jv *jsonValue) {
	*jv = jsonValue{}
	jsonValuePool.Put(jv)
}

func getEncodeState() *encodeState {
	return encodeStatePool.Get().(*encodeState)
}

// maxPooledEntries bounds the entries capacity a pooled state keeps, so one
// very wide object does not pin a large backing array in the pool.
const maxPooledEntries = 4096

// resetEncodeState drops every reference the state holds and reports whether
// it is small enough to return to the pool. Each object clears its segment
// before popping it, so only an encode that failed mid-object leaves entries
// below len; everything between len and cap is already zeroed.
func resetEncodeState(state *encodeState) bool {
	clear(state.visited)
	clear(state.entries)
	state.entries = state.entries[:0]
	return cap(state.entries) <= maxPooledEntries
}

func putEncodeState(state *encodeState) {
	if resetEncodeState(state) {
		encodeStatePool.Put(state)
	}
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
	globalSchemaCache *lru.Cache[[sha256.Size]byte, *jsonschema.Schema]
	schemaCacheOnce   sync.Once
)

const defaultSchemaCacheSize = 100

func initSchemaCache() {
	schemaCacheOnce.Do(func() {
		globalSchemaCache = lru.New[[sha256.Size]byte, *jsonschema.Schema](lru.WithCapacity(defaultSchemaCacheSize))
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

func evaluationResultError(result *jsonschema.EvaluationResult) error {
	if result == nil {
		return errors.New("validation failed")
	}
	details := result.DetailedErrors()
	if len(details) == 0 {
		return errors.New("validation failed")
	}

	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	first := keys[0]
	if len(keys) == 1 {
		return fmt.Errorf("%s: %s", first, details[first])
	}
	return fmt.Errorf("%s: %s (+%d more)", first, details[first], len(keys)-1)
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

	state := getEncodeState()
	defer putEncodeState(state)

	return encodeWithState(value, options, state)
}

func encodeWithState(value lua.LValue, options *EncodeOptions, state *encodeState) ([]byte, error) {
	jv := getJSONValue(value, state, 0, options)
	b, err := json.Marshal(jv)
	putJSONValue(jv)
	return b, err
}

type jsonValue struct {
	LValue  lua.LValue
	state   *encodeState
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
	if j.state.visited[table] {
		return nil, errNested
	}
	j.state.visited[table] = true
	defer delete(j.state.visited, table)

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

	// Hash keys are written in ascending byte order so equal tables encode to
	// identical bytes; Go map iteration order is randomized per traversal.
	// Nested objects push their segments above this one while it is written.
	start := len(j.state.entries)
	entries := j.state.entries

	if table.Strdict != nil {
		for key, value := range table.Strdict {
			if value != lua.LNil {
				entries = append(entries, objectEntry{key: key, value: value, rank: rankStrdict})
			}
		}
	}

	if table.Dict != nil {
		for key, value := range table.Dict {
			if value == lua.LNil {
				continue
			}
			switch k := key.(type) {
			case lua.LNumber:
				if isInteger(k) && k > 0 {
					continue // written with the numeric keys above
				}
				entries = append(entries, objectEntry{
					key: strconv.FormatFloat(float64(k), 'f', -1, 64), value: value, rank: rankDictNumber,
				})
			case lua.LString:
				entries = append(entries, objectEntry{key: string(k), value: value, rank: rankDictString})
			case lua.LBool:
				entries = append(entries, objectEntry{key: strconv.FormatBool(bool(k)), value: value, rank: rankDictBool})
			}
		}
	}

	j.state.entries = entries
	own := entries[start:]
	sortObjectEntries(own)

	for i := range own {
		if err := writeKeyValue(own[i].key, own[i].value); err != nil {
			return nil, err
		}
	}

	clear(j.state.entries[start:])
	j.state.entries = j.state.entries[:start]

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
		childJSON := getJSONValue(value, j.state, j.depth+1, j.options)
		childBytes, err := childJSON.MarshalJSON()
		putJSONValue(childJSON)
		if err != nil {
			return err
		}
		buf.Write(childBytes)
		return nil
	}
}

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
		return validationError(l, evaluationResultError(result), "validation failed")
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
		return validationError(l, evaluationResultError(result), "validation failed")
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
	cacheKey := sha256.Sum256(schemaJSON)
	if schema, ok := globalSchemaCache.Get(cacheKey); ok {
		return schema, nil
	}

	compiler := jsonschema.NewCompiler().WithDecoderJSON(decodeSchemaInstance)
	schema, err := compiler.Compile(schemaJSON)
	if err != nil {
		return nil, err
	}

	_ = globalSchemaCache.Set(cacheKey, schema)
	return schema, nil
}

func decodeSchemaInstance(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
