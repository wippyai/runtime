package lua

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
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
	fields := make([]fieldInfo, 0, rt.NumField())
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

	switch v.Type() {
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

		// Check array part directly instead of using MaxN()
		if tbl.Array != nil && len(tbl.Array) > 0 {
			maxIdx := 0
			for i := len(tbl.Array) - 1; i >= 0; i-- {
				if tbl.Array[i] != lua.LNil {
					maxIdx = i + 1
					break
				}
			}
			if maxIdx > 0 {
				return tableToSlice(tbl, maxIdx)
			}
		}
		return tableToMap(tbl)
	case lua.LTFunction, lua.LTUserData, lua.LTThread, lua.LTChannel:
		// FIXME rework on demand
		fallthrough
	default:
		return v.String()
	}
}

func tableToMap(tbl *lua.LTable) map[string]any {
	// Calculate capacity from all table parts
	capacity := 0
	if tbl.Array != nil {
		for _, v := range tbl.Array {
			if v != lua.LNil {
				capacity++
			}
		}
	}
	if tbl.Strdict != nil {
		capacity += len(tbl.Strdict)
	}
	if tbl.Dict != nil {
		capacity += len(tbl.Dict)
	}

	result := make(map[string]any, capacity)

	// Process array indices
	if tbl.Array != nil {
		for i, v := range tbl.Array {
			if v != lua.LNil {
				result[fmt.Sprintf("%d", i+1)] = ToGoAny(v)
			}
		}
	}

	// Process string keys directly
	if tbl.Strdict != nil {
		for k, v := range tbl.Strdict {
			if v != lua.LNil {
				result[k] = ToGoAny(v)
			}
		}
	}

	// Process non-string keys
	if tbl.Dict != nil {
		for k, v := range tbl.Dict {
			if v != lua.LNil {
				result[k.String()] = ToGoAny(v)
			}
		}
	}

	return result
}

func tableToSlice(tbl *lua.LTable, maxn int) []any {
	result := make([]any, maxn)
	for i := 0; i < maxn && i < len(tbl.Array); i++ {
		result[i] = ToGoAny(tbl.Array[i])
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
		return GoToLua(val.Data())
	case pubsub.PID:
		return lua.LString(val.String()), nil
	case []byte:
		return lua.LString(val), nil
	case error:
		ud := &lua.LUserData{
			Value:     errors.New(val),
			Metatable: value.GetTypeMetatable(nil, "error"),
		}
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
			return lua.LNil, nil
		}
		return sliceToTable(rv)

	case reflect.Map:
		if rv.IsNil() {
			return newTable(0, 0), nil
		}
		return mapToTable(rv)

	case reflect.Struct:
		return structToTable(rv)

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
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

func newTable(acap, hcap int) *lua.LTable {
	tb := &lua.LTable{
		Metatable: lua.LNil,
		Immutable: false,
	}
	if acap > 0 {
		tb.Array = make([]lua.LValue, 0, acap)
	}
	if hcap > 0 {
		tb.Strdict = make(map[string]lua.LValue, hcap)
	}
	return tb
}

func sliceToTable(rv reflect.Value) (lua.LValue, error) {
	length := rv.Len()
	table := newTable(length, 0)

	if length > 0 {
		table.Array = make([]lua.LValue, length)
		for i := 0; i < length; i++ {
			lval, err := GoToLua(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("error converting slice element %d: %w", i, err)
			}
			table.Array[i] = lval
		}
	}

	return table, nil
}

func mapToTable(rv reflect.Value) (lua.LValue, error) {
	length := rv.Len()
	table := newTable(0, length)

	if length > 0 {
		table.Strdict = make(map[string]lua.LValue, length)
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyStr := fmt.Sprint(key.Interface())

			lval, err := GoToLua(iter.Value().Interface())
			if err != nil {
				return nil, fmt.Errorf("error converting map value for key %s: %w", keyStr, err)
			}
			table.Strdict[keyStr] = lval
		}
	}

	return table, nil
}

func structToTable(rv reflect.Value) (lua.LValue, error) {
	typ := rv.Type()
	fields := getStructFields(typ)

	table := newTable(0, len(fields))
	if len(fields) > 0 {
		table.Strdict = make(map[string]lua.LValue, len(fields))

		for _, field := range fields {
			fieldValue := rv.Field(field.index)
			var lval lua.LValue
			var err error

			switch fieldValue.Kind() {
			case reflect.Map:
				if fieldValue.IsNil() {
					lval = newTable(0, 0)
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			case reflect.Ptr, reflect.Slice, reflect.Interface:
				if fieldValue.IsNil() {
					lval = lua.LNil
				} else {
					lval, err = GoToLua(fieldValue.Interface())
				}
			default:
				lval, err = GoToLua(fieldValue.Interface())
			}

			if err != nil {
				return nil, fmt.Errorf("error converting struct field %s: %w", field.name, err)
			}

			table.Strdict[field.name] = lval
		}
	}

	return table, nil
}
