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
	if v == nil {
		return lua.LNil, nil
	}

	// Handle basic types first
	switch val := v.(type) {
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
	case payload.Payload:
		return GoToLua(val.Data())
	case pid.PID:
		return lua.LString(val.String()), nil
	case []byte:
		return lua.LString(val), nil
	case error:
		// lua.Error implements LValue, metatable is set via builtinMts[LTUserData]
		return lua.WrapError(val, ""), nil
	}

	// Use reflection for complex types
	rv := reflect.ValueOf(v)
	//exhaustive:ignore
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return lua.LNil, nil
		}
		return GoToLua(rv.Elem().Interface())

	case reflect.Slice, reflect.Array:
		if rv.IsNil() {
			// Return nil for nil slices
			return lua.LNil, nil
		}
		table := lua.CreateTable(rv.Len(), 0)
		for i := 0; i < rv.Len(); i++ {
			lval, err := GoToLua(rv.Index(i).Interface())
			if err != nil {
				return nil, runtimelua.NewConversionError(fmt.Sprintf("error converting slice/array element %d", i), err)
			}
			table.RawSetInt(i+1, lval)
		}
		return table, nil

	case reflect.Map:
		if rv.IsNil() {
			// Return empty table for nil maps
			return lua.CreateTable(0, 0), nil
		}

		table := lua.CreateTable(0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyStr := fmt.Sprint(key.Interface())

			lval, err := GoToLua(iter.Value().Interface())
			if err != nil {
				return nil, runtimelua.NewConversionError(fmt.Sprintf("error converting map value for key %s", keyStr), err)
			}
			table.RawSetString(keyStr, lval)
		}
		return table, nil

	case reflect.Struct:
		typ := rv.Type()

		fields := getStructFields(typ)
		table := lua.CreateTable(0, len(fields))
		for _, field := range fields {
			fieldValue := rv.Field(field.index)
			var lval lua.LValue
			var err error

			//exhaustive:ignore
			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					lval = lua.CreateTable(0, 0) // Empty table for nil maps
					err = nil
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			case reflect.Pointer, reflect.Slice, reflect.Interface:
				if fieldValue.IsNil() {
					lval = lua.LNil // Explicit nil for other nil fields
					err = nil
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			default:
				lval, err = GoToLua(fieldValue.Interface())
			}

			if err != nil {
				return nil, runtimelua.NewConversionError(fmt.Sprintf("error converting struct field %s", field.name), err)
			}

			table.RawSetString(field.name, lval)
		}
		return table, nil

	default:
		return nil, runtimelua.NewUnsupportedTypeError(fmt.Sprintf("unsupported type: %T", v))
	}
}
