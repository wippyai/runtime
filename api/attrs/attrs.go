// Package attrs provides a unified key-value attribute bag for metadata, options, and configuration.
package attrs

import "time"

// Attributes provides a type-safe interface for accessing key-value pairs.
type Attributes interface {
	Get(key string) (any, bool)
	GetString(key string, def string) string
	GetInt(key string, def int) int
	GetBool(key string, def bool) bool
	GetDuration(key string, def time.Duration) time.Duration
	GetSlice(key string) []string
	GetBag(key string) (Bag, bool)
}

// Bag is a map-based implementation of Attributes.
type Bag map[string]any

// NewBag creates a new empty Bag.
func NewBag() Bag {
	return make(Bag)
}

// NewBagFrom creates a new Bag initialized with the provided data.
// The data is copied, not referenced.
func NewBagFrom(data map[string]any) Bag {
	b := make(Bag)
	if data != nil {
		for k, v := range data {
			b[k] = v
		}
	}
	return b
}

// Set stores a value for the given key.
func (b Bag) Set(key string, value any) {
	b[key] = value
}

// Get retrieves the value for the given key.
func (b Bag) Get(key string) (any, bool) {
	if b == nil {
		return nil, false
	}
	v, ok := b[key]
	return v, ok
}

// GetString retrieves the value as a string, returning def if not found or not a string.
func (b Bag) GetString(key string, def string) string {
	if v, ok := b.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// GetInt retrieves the value as an int, returning def if not found or not an int.
func (b Bag) GetInt(key string, def int) int {
	if v, ok := b.Get(key); ok {
		if i, ok := v.(int); ok {
			return i
		}
	}
	return def
}

// GetBool retrieves the value as a bool, returning def if not found or not a bool.
func (b Bag) GetBool(key string, def bool) bool {
	if v, ok := b.Get(key); ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// GetDuration retrieves the value as a time.Duration, returning def if not found or not a Duration.
func (b Bag) GetDuration(key string, def time.Duration) time.Duration {
	if v, ok := b.Get(key); ok {
		if d, ok := v.(time.Duration); ok {
			return d
		}
	}
	return def
}

// GetSlice retrieves the value as a string slice.
// It handles three cases:
//   - If the value is already a []string, returns it directly
//   - If the value is a single string, returns it as a single-element slice
//   - If the value is a []any containing strings, converts it to []string
//
// Returns nil if the key doesn't exist or the value cannot be converted to strings.
func (b Bag) GetSlice(key string) []string {
	if v, ok := b.Get(key); ok {
		// Case 1: Already a []string
		if s, ok := v.([]string); ok {
			return s
		}

		// Case 2: Single string
		if s, ok := v.(string); ok {
			return []string{s}
		}

		// Case 3: []any with strings
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

// GetBag retrieves the value as a Bag.
// It handles three cases:
//   - If the value is already a Bag, returns it directly
//   - If the value is a map[string]any, converts it to Bag
//   - If the value implements Attributes, attempts to convert to Bag
//
// Returns (nil, false) if the key doesn't exist or the value cannot be converted to Bag.
func (b Bag) GetBag(key string) (Bag, bool) {
	if v, ok := b.Get(key); ok {
		// Case 1: Already a Bag
		if bag, ok := v.(Bag); ok {
			return bag, true
		}

		// Case 2: map[string]any
		if m, ok := v.(map[string]any); ok {
			return Bag(m), true
		}

		// Case 3: Attributes interface (try type assertion to Bag)
		if attrs, ok := v.(Attributes); ok {
			if bag, ok := attrs.(Bag); ok {
				return bag, true
			}
		}
	}
	return nil, false
}

// Merge creates a new Bag with values from both this Bag and other.
// Values from other take precedence over values from this Bag.
func (b Bag) Merge(other Attributes) Bag {
	merged := NewBag()

	// Copy from this bag
	if b != nil {
		for k, v := range b {
			merged[k] = v
		}
	}

	// Copy from other bag (overwriting if keys conflict)
	if other != nil {
		if otherBag, ok := other.(Bag); ok {
			for k, v := range otherBag {
				merged[k] = v
			}
		}
	}

	return merged
}

// Clone creates a deep copy of the Bag.
// Returns any to satisfy the Cloner interface used in frame context inheritance.
func (b Bag) Clone() any {
	if b == nil {
		return NewBag()
	}

	cloned := NewBag()
	for k, v := range b {
		cloned[k] = v
	}

	return cloned
}

// Iterate calls the given function for each key-value pair in the Bag.
func (b Bag) Iterate(fn func(key string, value any)) {
	if b == nil {
		return
	}
	for k, v := range b {
		fn(k, v)
	}
}

// Len returns the number of key-value pairs in the Bag.
func (b Bag) Len() int {
	if b == nil {
		return 0
	}
	return len(b)
}

// Keys returns all keys in the Bag.
func (b Bag) Keys() []string {
	if b == nil {
		return nil
	}
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	return keys
}
