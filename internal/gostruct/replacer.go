package gostruct

import (
	"fmt"
	"reflect"
	"strings"
)

// Replacer is a helper struct for replacing values within arbitrary Go data structures.
type Replacer struct {
	replacements      map[string]string
	placeholderPrefix string
	placeholderSuffix string
}

// NewReplacer creates a new Replacer instance with the given replacements map.
func NewReplacer(replacements map[string]string) *Replacer {
	return &Replacer{
		replacements:      replacements,
		placeholderPrefix: "${",
		placeholderSuffix: "}",
	}
}

// replaceString replaces placeholders based on direct string matching.
func (r *Replacer) replaceString(s string) string {
	for key, value := range r.replacements {
		placeholder := r.placeholderPrefix + key + r.placeholderSuffix
		s = strings.ReplaceAll(s, placeholder, value)
	}
	return s
}

// replaceRecursive is the internal recursive function for value replacement.
func (r *Replacer) replaceRecursive(val reflect.Value) (any, error) {
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
			newKey, err := r.replaceRecursive(k) // Potentially replace in key
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

	case reflect.Slice:
		newSlice := reflect.MakeSlice(val.Type(), val.Len(), val.Cap())
		for i := 0; i < val.Len(); i++ {
			newValue, err := r.replaceRecursive(val.Index(i))
			if err != nil {
				return nil, err
			}
			newSlice.Index(i).Set(reflect.ValueOf(newValue))
		}
		return newSlice.Interface(), nil

	case reflect.Array:
		newArray := reflect.New(val.Type()).Elem() // Create a new array
		for i := 0; i < val.Len(); i++ {
			newValue, err := r.replaceRecursive(val.Index(i))
			if err != nil {
				return nil, err
			}
			newArray.Index(i).Set(reflect.ValueOf(newValue))
		}
		return newArray.Interface(), nil // Return the array

	case reflect.Struct:
		newStruct := reflect.New(val.Type()).Elem()
		for i := 0; i < val.NumField(); i++ {
			fieldVal := val.Field(i)
			fieldType := val.Type().Field(i)

			// Only process public fields
			if fieldType.IsExported() {
				newFieldVal, err := r.replaceRecursive(fieldVal)
				if err != nil {
					return nil, err
				}
				newStruct.Field(i).Set(reflect.ValueOf(newFieldVal))
			} else {
				// Handle unexported field without panic
				if !fieldVal.CanSet() {
					return nil, fmt.Errorf("cannot set unexported field '%s' in struct", fieldType.Name)
				}
				newStruct.Field(i).Set(fieldVal)
			}
		}
		return newStruct.Interface(), nil

	case reflect.String:
		return r.replaceString(val.String()), nil

	default:
		return val.Interface(), nil
	}
}

// Replace recursively searches for values in 'data' and replaces them
// based on the 'replacements' map within the Replacer instance.
func (r *Replacer) Replace(data any) (any, error) {
	return r.replaceRecursive(reflect.ValueOf(data))
}
