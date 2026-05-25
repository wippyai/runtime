// SPDX-License-Identifier: MPL-2.0

package registry

import "reflect"

// mapsEqual compares two maps for equality while ignoring key order
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}

	for k, aVal := range a {
		bVal, exists := b[k]
		if !exists {
			return false
		}

		if aMap, aOk := aVal.(map[string]any); aOk {
			if bMap, bOk := bVal.(map[string]any); bOk {
				if !mapsEqual(aMap, bMap) {
					return false
				}
				continue
			}
			return false
		}

		if aArr, aOk := aVal.([]any); aOk {
			if bArr, bOk := bVal.([]any); bOk {
				if len(aArr) != len(bArr) {
					return false
				}
				for i := range aArr {
					if !valuesEqual(aArr[i], bArr[i]) {
						return false
					}
				}
				continue
			}

			return false
		}

		if !valuesEqual(aVal, bVal) {
			return false
		}
	}

	return true
}

// valuesEqual compares two values of any type
func valuesEqual(a, b any) bool {
	if aMap, aOk := a.(map[string]any); aOk {
		if bMap, bOk := b.(map[string]any); bOk {
			return mapsEqual(aMap, bMap)
		}
		return false
	}

	if aArr, aOk := a.([]any); aOk {
		if bArr, bOk := b.([]any); bOk {
			if len(aArr) != len(bArr) {
				return false
			}
			for i := range aArr {
				if !valuesEqual(aArr[i], bArr[i]) {
					return false
				}
			}
			return true
		}
		return false
	}

	if isNumeric(a) && isNumeric(b) {
		return toFloat64(a) == toFloat64(b)
	}

	return a == b
}

// isNumeric checks if a value is a numeric type
func isNumeric(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// toFloat64 converts a numeric value to float64
func toFloat64(v any) float64 {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Invalid, reflect.Bool, reflect.String, reflect.Chan, reflect.Func, reflect.Pointer,
		reflect.Interface, reflect.Array, reflect.Map, reflect.Slice, reflect.Struct,
		reflect.UnsafePointer, reflect.Uintptr, reflect.Complex64, reflect.Complex128:
		return 0
	default:
		return 0
	}
}
