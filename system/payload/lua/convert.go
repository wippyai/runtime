package lua

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/errors"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"

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
	var fields []fieldInfo
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

// ToGoAny converts a lua.LValue to its Go equivalent.
func ToGoAny(v lua.LValue) any {
	if v == nil {
		return nil
	}

	switch v.Type() { //nolint:exhaustive
	case lua.LTNil:
		return nil
	case lua.LTBool:
		return lua.LVAsBool(v)
	case lua.LTNumber:
		return float64(v.(lua.LNumber))
	case lua.LTString:
		return string(v.(lua.LString))
	case lua.LTTable:
		tbl := v.(*lua.LTable)
		maxn := tbl.MaxN()
		if maxn == 0 {
			return tableToMap(tbl)
		}
		return tableToSlice(tbl, maxn)
	default:
		return v.String()
	}
}

func tableToMap(tbl *lua.LTable) map[string]any {
	result := make(map[string]any)
	tbl.ForEach(func(key, value lua.LValue) {
		result[key.String()] = ToGoAny(value)
	})
	return result
}

func tableToSlice(tbl *lua.LTable, maxn int) []any {
	result := make([]any, 0, maxn)
	for i := 1; i <= maxn; i++ {
		result = append(result, ToGoAny(tbl.RawGetInt(i)))
	}
	return result
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
		return GoToLua(val.(payload.Payload).Data())
	case pubsub.PID:
		return lua.LString(val.String()), nil
	case []byte:
		return lua.LString(val), nil
	case error:
		ud := engine.SharedState.NewUserData()
		ud.Value = errors.New(val)
		ud.Metatable = value.GetTypeMetatable(engine.SharedState, "error")

		return ud, nil
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
		table := engine.SharedState.NewTable()
		for i := 0; i < rv.Len(); i++ {
			lval, err := GoToLua(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("error converting slice/array element %d: %w", i, err)
			}
			table.RawSetInt(i+1, lval)
		}
		return table, nil

	case reflect.Map:
		if rv.IsNil() {
			// Return empty table for nil maps
			return engine.SharedState.NewTable(), nil
		}

		table := engine.SharedState.CreateTable(0, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyStr := fmt.Sprint(key.Interface())

			lval, err := GoToLua(iter.Value().Interface())
			if err != nil {
				return nil, fmt.Errorf("error converting map value for key %s: %w", keyStr, err)
			}
			table.RawSetString(keyStr, lval)
		}
		return table, nil

	case reflect.Struct:
		typ := rv.Type()

		fields := getStructFields(typ)
		table := engine.SharedState.CreateTable(0, len(fields))
		for _, field := range fields {
			fieldValue := rv.Field(field.index)
			var lval lua.LValue
			var err error

			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					lval = engine.SharedState.NewTable() // Empty table for nil maps
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
			default:
				lval, err = GoToLua(fieldValue.Interface())
			}

			if err != nil {
				return nil, fmt.Errorf("error converting struct field %s: %w", field.name, err)
			}

			table.RawSetString(field.name, lval)
		}
		return table, nil

	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}
