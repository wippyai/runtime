package policy

// matchesPattern checks if a pattern matches a value
// Supports exact match, wildcard "*", and suffix wildcard "prefix*" or "prefix.*"
func matchesPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}

	if pattern == value {
		return true
	}

	if len(pattern) > 2 && pattern[len(pattern)-2:] == ".*" {
		prefix := pattern[:len(pattern)-2]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	if len(pattern) > 1 && pattern[len(pattern)-1:] == "*" {
		prefix := pattern[:len(pattern)-1]
		return len(value) >= len(prefix) && value[:len(prefix)] == prefix
	}

	return false
}

// matchesFilter checks if a filter (string or []any) matches a value
func matchesFilter(filter any, value string) bool {
	switch f := filter.(type) {
	case string:
		if f == "*" {
			return true
		}
		return matchesPattern(f, value)
	case []any:
		for _, item := range f {
			if str, ok := item.(string); ok {
				if str == "*" {
					return true
				}
				if matchesPattern(str, value) {
					return true
				}
			}
		}
	case []string:
		for _, str := range f {
			if str == "*" {
				return true
			}
			if matchesPattern(str, value) {
				return true
			}
		}
	}
	return false
}
