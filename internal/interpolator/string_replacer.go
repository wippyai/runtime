// SPDX-License-Identifier: MPL-2.0

package interpolator

import (
	"reflect"
)

// stringReplacer defines the function signature for custom string replacement logic with context.
type stringReplacer func(string, any) (string, error)

// replacer handles string replacement within arbitrary data structures.
type replacer struct {
	rFunc stringReplacer
}

// newStringReplacer creates a new replacer with the provided replacement function.
func newStringReplacer(replacementFunc stringReplacer) *replacer {
	return &replacer{
		rFunc: replacementFunc,
	}
}

// replaceString applies the replacement function to the given string, passing the context.
func (r *replacer) replaceString(s string, ctx any) (string, error) {
	if r.rFunc == nil {
		return s, nil
	}
	return r.rFunc(s, ctx)
}

// replaceRecursive is the helpers recursive function for value replacement with context.
func (r *replacer) replaceRecursive(val reflect.Value, ctx any) (any, error) {
	if !val.IsValid() {
		return nil, nil //nolint:nilnil // nil passthrough for invalid values
	}

	switch val.Kind() {
	case reflect.Pointer, reflect.Interface:
		if val.IsNil() {
			return val.Interface(), nil
		}
		elem, err := r.replaceRecursive(val.Elem(), ctx)
		if err != nil {
			return nil, err
		}
		if val.Kind() == reflect.Pointer {
			ptr := reflect.New(val.Type().Elem())
			ptr.Elem().Set(reflect.ValueOf(elem))
			return ptr.Interface(), nil
		}
		return elem, nil

	case reflect.Map:
		newMap := reflect.MakeMap(val.Type())
		for _, k := range val.MapKeys() {
			v := val.MapIndex(k)
			newKey, err := r.replaceRecursive(k, ctx)
			if err != nil {
				return nil, err
			}
			newValue, err := r.replaceRecursive(v, ctx)
			if err != nil {
				return nil, err
			}
			newMap.SetMapIndex(reflect.ValueOf(newKey), reflect.ValueOf(newValue))
		}
		return newMap.Interface(), nil

	case reflect.Slice, reflect.Array:
		var newSliceOrArray reflect.Value
		if val.Kind() == reflect.Slice {
			newSliceOrArray = reflect.MakeSlice(val.Type(), val.Len(), val.Cap())
		} else {
			newSliceOrArray = reflect.New(val.Type()).Elem()
		}
		for i := 0; i < val.Len(); i++ {
			newValue, err := r.replaceRecursive(val.Index(i), ctx)
			if err != nil {
				return nil, err
			}
			newSliceOrArray.Index(i).Set(reflect.ValueOf(newValue))
		}
		return newSliceOrArray.Interface(), nil

	case reflect.Struct:
		newStruct := reflect.New(val.Type()).Elem()
		for i := 0; i < val.NumField(); i++ {
			if val.Type().Field(i).IsExported() {
				fieldVal := val.Field(i)
				newValue, err := r.replaceRecursive(fieldVal, ctx)
				if err != nil {
					return nil, err
				}
				newStruct.Field(i).Set(reflect.ValueOf(newValue))
			}
		}
		return newStruct.Interface(), nil

	case reflect.String:
		newStr, err := r.replaceString(val.String(), ctx)
		if err != nil {
			return nil, err
		}
		return newStr, nil
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		fallthrough
	default:
		return val.Interface(), nil
	}
}

// Replace is the main entry point for string replacement within 'data' with context.
func (r *replacer) Replace(data any, ctx any) (any, error) {
	if data == nil {
		return nil, nil //nolint:nilnil // nil passthrough for nil input
	}

	result, err := r.replaceRecursive(reflect.ValueOf(data), ctx)
	if err != nil {
		return nil, err
	}
	return result, nil
}
