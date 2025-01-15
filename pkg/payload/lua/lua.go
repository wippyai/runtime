package lua

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
)

func Register(transcoder payload.TranscoderRegister) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.Lua, payload.Golang, 2, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.Lua, 2, from)
	transcoder.RegisterUnmarshaler(payload.Lua, to)
}

// ToGolang converts a Lua payload to a Golang payload.
// It also implements the payload.Unmarshaler interface for Lua payloads.
type ToGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *ToGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Lua {
		return nil, fmt.Errorf("Lua=>Golang can only transcode from Lua format, got %s", p.Format())
	}

	l := lua.NewState()
	defer l.Close()

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return nil, fmt.Errorf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data())
	}

	data := ToGoAny(lv)

	return payload.NewPayload(data, payload.Golang), nil
}

// Unmarshal implements the payload.Unmarshaler interface.
func (t *ToGolang) Unmarshal(p payload.Payload, v interface{}) error {
	if p.Format() != payload.Lua {
		return fmt.Errorf("Lua=>Golang can only unmarshal from Lua format, got %s", p.Format())
	}

	l := lua.NewState()
	defer l.Close()

	lv, ok := p.Data().(lua.LValue)
	if !ok {
		return fmt.Errorf("Lua=>Golang expects data to be of type lua.LValue, got %T", p.Data())
	}

	val := ToGoAny(lv)

	targetValue := reflect.ValueOf(v)
	if targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		return fmt.Errorf("target must be a non-nil pointer, got %s", targetValue.Type())
	}

	return unmarshalRecursive(val, targetValue.Elem())
}

func unmarshalRecursive(val interface{}, targetValue reflect.Value) error {
	switch targetValue.Kind() {
	case reflect.Ptr:
		// Handle pointers by creating a new value and recursively unmarshalling into it
		if targetValue.IsNil() {
			targetValue.Set(reflect.New(targetValue.Type().Elem()))
		}
		return unmarshalRecursive(val, targetValue.Elem())

	case reflect.Struct:
		// Handle structs
		mapVal, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("cannot assign value of type %s to struct %s", reflect.TypeOf(val), targetValue.Type())
		}

		targetType := targetValue.Type()
		for i := 0; i < targetType.NumField(); i++ {
			field := targetType.Field(i)
			fieldValue := targetValue.Field(i)

			luaTag := field.Tag.Get("lua")
			jsonTag := field.Tag.Get("json")

			// Determine the key to use for lookup in the map
			var keyToUse string
			if luaTag != "" {
				keyToUse = luaTag
			} else if jsonTag != "" {
				// Handle json tag with options (e.g., ",omitempty")
				keyToUse = strings.Split(jsonTag, ",")[0]
			} else {
				keyToUse = field.Name // Fallback to field name
			}

			foundMatch := false
			for k, v := range mapVal {
				if strings.EqualFold(keyToUse, k) {
					if err := unmarshalRecursive(v, fieldValue); err != nil {
						return fmt.Errorf("error unmarshalling field %s: %w", field.Name, err)
					}
					foundMatch = true
					break
				}
			}

			// If no match was found and json tag was used with "omitempty", it's okay
			if !foundMatch && jsonTag != "" && strings.Contains(jsonTag, "omitempty") {
				continue
			}

			// If no match was found, and a json tag was used without "omitempty", return an error
			if !foundMatch && jsonTag != "" && !strings.Contains(jsonTag, "omitempty") {
				// Only return error if lua tag is not present
				if luaTag == "" {
					return fmt.Errorf("json tag '%s' specified for field %s, but no matching key found in Lua table", jsonTag, field.Name)
				}
			}
		}

	case reflect.Slice:
		// Handle slices
		sliceVal, ok := val.([]interface{})
		if !ok {
			return fmt.Errorf("cannot assign value of type %s to slice %s", reflect.TypeOf(val), targetValue.Type())
		}

		targetValue.Set(reflect.MakeSlice(targetValue.Type(), len(sliceVal), len(sliceVal)))
		for i, v := range sliceVal {
			if err := unmarshalRecursive(v, targetValue.Index(i)); err != nil {
				return fmt.Errorf("error unmarshalling element at index %d: %w", i, err)
			}
		}

	case reflect.Map:
		// Handle maps
		mapVal, ok := val.(map[string]interface{})
		if !ok {
			return fmt.Errorf("cannot assign value of type %s to map %s", reflect.TypeOf(val), targetValue.Type())
		}

		if targetValue.IsNil() {
			targetValue.Set(reflect.MakeMap(targetValue.Type()))
		}

		for k, v := range mapVal {
			mapKey := reflect.ValueOf(k)
			if !mapKey.Type().AssignableTo(targetValue.Type().Key()) {
				return fmt.Errorf("cannot use map key of type %s as key for map of type %s", mapKey.Type(), targetValue.Type())
			}

			mapValue := reflect.New(targetValue.Type().Elem()).Elem()
			if err := unmarshalRecursive(v, mapValue); err != nil {
				return fmt.Errorf("error unmarshalling map value for key %s: %w", k, err)
			}
			targetValue.SetMapIndex(mapKey, mapValue)
		}

	case reflect.Interface:
		// Handle interfaces by assigning the value directly
		targetValue.Set(reflect.ValueOf(val))

	default:
		// Handle primitive types and other cases not covered above
		sourceValue := reflect.ValueOf(val)
		if !sourceValue.Type().AssignableTo(targetValue.Type()) {
			// Handle numeric type conversions
			if sourceValue.Kind() == reflect.Float64 && targetValue.Kind() == reflect.Int {
				floatVal := sourceValue.Float()
				intVal := int64(floatVal)

				// Check for precision loss using a tolerance
				tolerance := 1e-14 // https://en.wikipedia.org/wiki/Machine_epsilon => 16
				if math.Abs(floatVal-float64(intVal)) > tolerance {
					return fmt.Errorf("cannot assign float64 value %v to int field without precision loss", floatVal)
				}

				targetValue.SetInt(intVal)
				return nil
			}
			return fmt.Errorf("cannot assign value of type %s to %s", sourceValue.Type(), targetValue.Type())
		}
		targetValue.Set(sourceValue)
	}

	return nil
}

// FromGolang converts a Golang payload to a Lua payload.
type FromGolang struct{}

// Transcode implements the payload.FormatTranscoder interface.
func (t *FromGolang) Transcode(p payload.Payload) (payload.Payload, error) {
	if p.Format() != payload.Golang {
		return nil, fmt.Errorf("Golang=>Lua can only transcode from Golang format, got %s", p.Format())
	}

	l := lua.NewState()
	defer l.Close()

	lv := GoToLua(l, p.Data())

	return payload.NewPayload(lv, payload.Lua), nil
}
