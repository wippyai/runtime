package gostruct

import (
	"reflect"
)

// StringReplacementFunc defines the function signature for custom string replacement logic.
type StringReplacementFunc func(string) (string, error)

// StringReplacer handles string replacement within arbitrary data structures.
type StringReplacer struct {
	replacementFunc StringReplacementFunc
}

// NewStringReplacer creates a new StringReplacer with the provided replacement function.
func NewStringReplacer(replacementFunc StringReplacementFunc) *StringReplacer {
	return &StringReplacer{
		replacementFunc: replacementFunc,
	}
}

// replaceString applies the replacement function to the given string.
func (r *StringReplacer) replaceString(s string) (string, error) {
	if r.replacementFunc == nil {
		return s, nil
	}
	return r.replacementFunc(s)
}

// replaceRecursive is the internal recursive function for value replacement.
func (r *StringReplacer) replaceRecursive(val reflect.Value) (any, error) {
	if !val.IsValid() {
		return nil, nil
	}

	switch val.Kind() {
	case reflect.Ptr, reflect.Interface:
		if val.IsNil() {
			return val.Interface(), nil
		}
		elem, err := r.replaceRecursive(val.Elem())
		if err != nil {
			return nil, err
		}
		if val.Kind() == reflect.Ptr {
			ptr := reflect.New(val.Type().Elem())
			ptr.Elem().Set(reflect.ValueOf(elem))
			return ptr.Interface(), nil
		}
		return elem, nil

	case reflect.Map:
		newMap := reflect.MakeMap(val.Type())
		for _, k := range val.MapKeys() {
			v := val.MapIndex(k)
			newKey, err := r.replaceRecursive(k)
			if err != nil {
				return nil, err
			}
			newValue, err := r.replaceRecursive(v)
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
			newValue, err := r.replaceRecursive(val.Index(i))
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
				newValue, err := r.replaceRecursive(fieldVal)
				if err != nil {
					return nil, err
				}
				newStruct.Field(i).Set(reflect.ValueOf(newValue))
			}
		}
		return newStruct.Interface(), nil

	case reflect.String:
		newStr, err := r.replaceString(val.String())
		if err != nil {
			return nil, err
		}
		return newStr, nil

	default:
		return val.Interface(), nil
	}
}

// Replace is the main entry point for string replacement within 'data'.
func (r *StringReplacer) Replace(data any) (any, error) {
	if data == nil {
		return nil, nil
	}

	result, err := r.replaceRecursive(reflect.ValueOf(data))
	if err != nil {
		return nil, err
	}
	return result, nil
}
