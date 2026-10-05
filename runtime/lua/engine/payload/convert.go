// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	runtimelua "github.com/wippyai/runtime/runtime/lua"

	lua "github.com/wippyai/go-lua"
)

// fieldInfo holds cached information about a struct field.
type fieldInfo struct {
	name  string // resolved field name (using json tag if available)
	index int
}

var structFieldCache sync.Map // map[reflect.Type][]fieldInfo

const maxLuaInteger = ^uint64(0) >> 1

// getStructFields returns cached field info for a given struct type.
func getStructFields(rt reflect.Type) []fieldInfo {
	if cached, ok := structFieldCache.Load(rt); ok {
		return cached.([]fieldInfo)
	}
	fields := make([]fieldInfo, 0)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldName := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			fieldName = tag
		}
		fields = append(fields, fieldInfo{name: fieldName, index: i})
	}
	structFieldCache.Store(rt, fields)
	return fields
}

// GoToLua converts a Go value to its Lua equivalent.
func GoToLua(v any) (lua.LValue, error) {
	converter := goToLuaConverter{}
	return converter.convert(v)
}

// goToLuaConverter builds Lua values directly. FromGolang enables its special
// value conventions without first copying the input into a normalized Go tree.
type goToLuaConverter struct {
	context   *payload.TranscodeContext
	normalize bool
	// Nested transcoder failures historically pass through without container
	// conversion wrappers; keep that distinction in the single-pass walk.
	nestedError bool
}

func (c *goToLuaConverter) conversionError(message string, err error) error {
	if c.nestedError {
		return err
	}
	return runtimelua.NewConversionError(message, err)
}

func (c *goToLuaConverter) convert(v any) (lua.LValue, error) {
	if v == nil {
		return lua.LNil, nil
	}

	// Handle basic types first
	switch val := v.(type) {
	case payload.Payload:
		if !c.normalize {
			// GoToLua prefers existing Lua values; FromGolang resolves payload
			// wrappers first, including types implementing both interfaces.
			if lv, ok := val.(lua.LValue); ok {
				return lv, nil
			}
			return c.convert(val.Data())
		}
		data, err := normalizeNestedPayload(c.context, val)
		if err != nil {
			c.nestedError = true
			return nil, err
		}
		// Direct use without a parent retains the raw-data fallback.
		return GoToLua(data)
	case lua.LValue:
		return val, nil
	case string:
		return lua.LString(val), nil
	case float64:
		return lua.LNumber(val), nil
	case float32:
		return lua.LNumber(val), nil
	case int:
		return lua.LInteger(val), nil
	case int32:
		return lua.LInteger(val), nil
	case int64:
		return lua.LInteger(val), nil
	case uint:
		if uint64(val) > maxLuaInteger {
			return nil, runtimelua.NewUnsupportedTypeError("unsupported value: uint overflows lua integer")
		}
		return lua.LInteger(int64(val)), nil
	case uint8:
		return lua.LInteger(int64(val)), nil
	case uint16:
		return lua.LInteger(int64(val)), nil
	case uint32:
		return lua.LInteger(int64(val)), nil
	case uint64:
		if val > maxLuaInteger {
			return nil, runtimelua.NewUnsupportedTypeError("unsupported value: uint64 overflows lua integer")
		}
		return lua.LInteger(int64(val)), nil
	case bool:
		return lua.LBool(val), nil
	case time.Time:
		return lua.LNumber(val.Unix()), nil
	case time.Duration:
		if c.normalize {
			return lua.LInteger(val), nil
		}
	case pid.PID:
		return lua.LString(val.String()), nil
	case []byte:
		return lua.LString(val), nil
	case error:
		// lua.Error implements LValue, metatable is set via builtinMts[LTUserData]
		return lua.WrapError(val, ""), nil
	case map[string]any:
		table := lua.CreateTable(0, max(1, len(val)))
		for key, value := range val {
			lv, err := c.convert(value)
			if err != nil {
				return nil, c.conversionError(fmt.Sprintf("error converting map value for key %s", key), err)
			}
			table.RawSetString(key, lv)
		}
		return table, nil
	case []any:
		if val == nil {
			return lua.LNil, nil
		}
		table := lua.CreateTable(max(1, len(val)), 0)
		for i, value := range val {
			lv, err := c.convert(value)
			if err != nil {
				return nil, c.conversionError(fmt.Sprintf("error converting slice/array element %d", i), err)
			}
			table.RawSetInt(i+1, lv)
		}
		return table, nil
	}

	// Use reflection for complex types
	rv := reflect.ValueOf(v)
	//exhaustive:ignore
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return lua.LNil, nil
		}
		return c.convert(rv.Elem().Interface())

	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			// Return nil for nil slices
			return lua.LNil, nil
		}
		// An empty Go slice still reaches Lua as a list, so reserve one array
		// slot even when the slice has no elements.
		table := lua.CreateTable(max(1, rv.Len()), 0)
		for i := 0; i < rv.Len(); i++ {
			lval, err := c.convert(rv.Index(i).Interface())
			if err != nil {
				return nil, c.conversionError(fmt.Sprintf("error converting slice/array element %d", i), err)
			}
			table.RawSetInt(i+1, lval)
		}
		return table, nil

	case reflect.Map:
		if rv.IsNil() {
			// Return empty object-shaped table for nil maps. Allocating one
			// hash slot keeps the empty table an object through a round trip.
			return lua.CreateTable(0, 1), nil
		}

		// An empty Go map still reaches Lua as an object, so reserve one hash
		// slot even when the map has no entries.
		table := lua.CreateTable(0, max(1, rv.Len()))
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyStr := fmt.Sprint(key.Interface())

			lval, err := c.convert(iter.Value().Interface())
			if err != nil {
				return nil, c.conversionError(fmt.Sprintf("error converting map value for key %s", keyStr), err)
			}
			table.RawSetString(keyStr, lval)
		}
		return table, nil

	case reflect.Struct:
		typ := rv.Type()

		fields := getStructFields(typ)
		size := len(fields)
		if c.normalize {
			size = max(1, size)
		}
		table := lua.CreateTable(0, size)
		for _, field := range fields {
			fieldValue := rv.Field(field.index)
			var lval lua.LValue
			var err error

			//exhaustive:ignore
			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					lval = lua.CreateTable(0, 1) // Empty object for nil maps
					err = nil
				} else {
					lval, err = c.convert(fieldValue.Interface())
				}
			case reflect.Pointer, reflect.Slice, reflect.Interface:
				if fieldValue.IsNil() {
					lval = lua.LNil // Explicit nil for other nil fields
					err = nil
				} else {
					lval, err = c.convert(fieldValue.Interface())
				}
			default:
				lval, err = c.convert(fieldValue.Interface())
			}

			if err != nil {
				if c.normalize {
					return nil, c.conversionError(fmt.Sprintf("error converting map value for key %s", field.name), err)
				}
				return nil, c.conversionError(fmt.Sprintf("error converting struct field %s", field.name), err)
			}

			table.RawSetString(field.name, lval)
		}
		return table, nil

	default:
		return nil, runtimelua.NewUnsupportedTypeError(fmt.Sprintf("unsupported type: %T", v))
	}
}
