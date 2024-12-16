package lua

import (
	"fmt"
	"reflect"

	"github.com/ponyruntime/go-lua"
	"github.com/ponyruntime/pony/api/payload"
)

func Register(transcoder payload.Transcoder) {
	to := &ToGolang{}
	from := &FromGolang{}

	transcoder.RegisterTranscoder(payload.Lua, payload.Golang, 1, to)
	transcoder.RegisterTranscoder(payload.Golang, payload.Lua, 1, from)
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
			tag := field.Tag.Get("lua")
			if tag == "" {
				continue
			}

			if mapValue, ok := mapVal[tag]; ok {
				fieldValue := targetValue.Field(i)
				if err := unmarshalRecursive(mapValue, fieldValue); err != nil {
					return fmt.Errorf("error unmarshalling field %s: %w", field.Name, err)
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
				if floatVal != float64(int(floatVal)) {
					return fmt.Errorf("cannot assign float64 value %v to int field without precision loss", floatVal)
				}
				targetValue.SetInt(int64(floatVal))
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
