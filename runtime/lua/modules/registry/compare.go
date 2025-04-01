package registry

// mapsEqual compares two maps for equality while ignoring key order
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}

	for k, aVal := range a {
		bVal, exists := b[k]
		if !exists {
			return false // Key doesn't exist in map b
		}

		// Handle nested maps recursively
		if aMap, aOk := aVal.(map[string]any); aOk {
			if bMap, bOk := bVal.(map[string]any); bOk {
				if !mapsEqual(aMap, bMap) {

					return false
				}
				continue
			}
			return false // Types don't match
		}

		// Handle arrays/slices
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

			return false // Types don't match
		}

		// Simple value comparison for primitive types
		if !valuesEqual(aVal, bVal) {
			return false
		}
	}

	return true
}

// valuesEqual compares two values of any type
func valuesEqual(a, b interface{}) bool {
	// Handle nested maps
	if aMap, aOk := a.(map[string]any); aOk {
		if bMap, bOk := b.(map[string]any); bOk {
			return mapsEqual(aMap, bMap)
		}
		return false
	}

	// Handle arrays/slices
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

	// For numbers, convert to float64 for comparison
	if isNumber(a) && isNumber(b) {
		return toFloat64(a) == toFloat64(b)
	}

	// Direct comparison for other types
	return a == b
}

// isNumber checks if a value is a numeric type
func isNumber(v interface{}) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

// toFloat64 converts a numeric value to float64
func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case float64:
		return val
	// Add other numeric types as needed
	default:
		return 0
	}
}
