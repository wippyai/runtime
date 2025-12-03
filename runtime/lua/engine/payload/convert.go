package payload

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"

	lua "github.com/yuin/gopher-lua"
)

// fieldInfo holds cached information about a struct field.
type fieldInfo struct {
	name  string // resolved field name (using json tag if available)
	index int
}

var structFieldCache sync.Map // map[reflect.Type][]fieldInfo

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
	case int:
		return lua.LNumber(val), nil
	case int32, int64:
		return lua.LNumber(reflect.ValueOf(val).Int()), nil
	case bool:
		return lua.LBool(val), nil
	case time.Time:
		return lua.LNumber(val.Unix()), nil
	case payload.Payload:
		return GoToLua(val.Data())
	case relay.PID:
		return lua.LString(val.String()), nil
	case []byte:
		return lua.LString(val), nil
	case error:
		// lua.Error implements LValue, so we can return it directly
		return lua.WrapError(val, ""), nil
	}

	// Use reflection for complex types
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr:
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
				return nil, NewConversionError(fmt.Sprintf("error converting slice/array element %d", i), err)
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
				return nil, NewConversionError(fmt.Sprintf("error converting map value for key %s", keyStr), err)
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

			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					lval = lua.CreateTable(0, 0) // Empty table for nil maps
					err = nil
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			case reflect.Ptr, reflect.Slice, reflect.Interface:
				if fieldValue.IsNil() {
					lval = lua.LNil // Explicit nil for other nil fields
					err = nil
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			case reflect.Invalid,
				reflect.Bool,
				reflect.Int,
				reflect.Int8,
				reflect.Int16,
				reflect.Int32,
				reflect.Int64,
				reflect.Uint,
				reflect.Uint8,
				reflect.Uint16,
				reflect.Uint32,
				reflect.Uint64,
				reflect.Uintptr,
				reflect.Float32,
				reflect.Float64,
				reflect.Complex64,
				reflect.Complex128,
				reflect.Array,
				reflect.Chan,
				reflect.Func,
				reflect.String,
				reflect.Struct,
				reflect.UnsafePointer:
				// FIXME rework on demand
				fallthrough

			default:
				lval, err = GoToLua(fieldValue.Interface())
			}

			if err != nil {
				return nil, NewConversionError(fmt.Sprintf("error converting struct field %s", field.name), err)
			}

			table.RawSetString(field.name, lval)
		}
		return table, nil

	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.String,
		reflect.UnsafePointer:
		// FIXME rework on demand
		fallthrough
	default:
		return nil, NewUnsupportedTypeError(fmt.Sprintf("unsupported type: %T", v))
	}
}
