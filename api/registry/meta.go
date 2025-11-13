// Package registry provides service registry and entry management.
package registry

// Metadata is a map for storing arbitrary key-value metadata associated with an entry.
// This can include any additional information relevant to the entry.
type Metadata map[string]any

// StringValue retrieves the value associated with the given key as a string.
// If the key doesn't exist or the value is not a string, it returns an empty string.
func (m Metadata) StringValue(key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// IntValue retrieves the value associated with the given key as an integer.
// If the key doesn't exist or the value is not an integer, it returns 0.
func (m Metadata) IntValue(key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return 0
}

// BoolValue retrieves the value associated with the given key as a boolean.
// If the key doesn't exist or the value is not a boolean, it returns false.
func (m Metadata) BoolValue(key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// TagValue retrieves the value associated with the given key as a string slice.
// It handles three cases:
//   - If the value is already a []string, returns it directly
//   - If the value is a single string, returns it as a single-element slice
//   - If the value is a []any containing strings, converts it to []string
//
// Returns nil if the key doesn't exist or the value cannot be converted to strings.
func (m Metadata) TagValue(key string) []string {
	if v, ok := m[key]; ok {
		// Case 1: Already a []string
		if s, ok := v.([]string); ok {
			return s
		}

		// Case 2: Single string
		if s, ok := v.(string); ok {
			return []string{s}
		}

		if arr, ok := v.([]any); ok {
			result := make([]string, len(arr))
			for i, val := range arr {
				if str, ok := val.(string); ok {
					result[i] = str
				}
			}
			return result
		}
	}
	return nil
}
