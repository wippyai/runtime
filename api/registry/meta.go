package registry

// Metadata is a map for storing arbitrary key-value metadata associated with an entry.
// This can include any additional information relevant to the entry.
type Metadata map[string]any

func (m Metadata) StringValue(key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (m Metadata) IntValue(key string) int {
	if v, ok := m[key]; ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return 0
}

func (m Metadata) BoolValue(key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

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
