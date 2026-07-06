// SPDX-License-Identifier: MPL-2.0

// Package attrs provides a unified key-value attribute bag for metadata, options, and configuration.
package attrs

import (
	"encoding/json"
	"math"
	"time"
)

type (
	// Attributes provides a type-safe interface for accessing key-value pairs.
	Attributes interface {
		Get(key string) (any, bool)
		GetString(key string, def string) string
		GetInt(key string, def int) int
		GetFloat(key string, def float64) float64
		GetBool(key string, def bool) bool
		GetDuration(key string, def time.Duration) time.Duration
		GetSlice(key string) []string
		GetBag(key string) (Bag, bool)
	}

	// Bag is a map-based implementation of Attributes.
	Bag map[string]any
)

const (
	intMax          = int64(^uint(0) >> 1)
	intMin          = -intMax - 1
	maxSafeFloatInt = float64(1<<53 - 1)
)

// NewBag creates a new empty Bag.
func NewBag() Bag {
	return make(Bag)
}

// NewBagFrom creates a new Bag initialized with the provided data.
// The data is copied, not referenced.
func NewBagFrom(data map[string]any) Bag {
	b := make(Bag)
	for k, v := range data {
		b[k] = v
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
		if i, ok := AsInt(v); ok {
			return i
		}
	}
	return def
}

// AsInt returns numeric values as int when the conversion is exact and in range.
func AsInt(value any) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return intFromInt64(n)
	case uint:
		return intFromUint64(uint64(n))
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return intFromUint64(uint64(n))
	case uint64:
		return intFromUint64(n)
	case float32:
		return intFromFloat64(float64(n))
	case float64:
		return intFromFloat64(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return intFromInt64(i)
	default:
		return 0, false
	}
}

func intFromInt64(n int64) (int, bool) {
	if n < intMin || n > intMax {
		return 0, false
	}
	return int(n), true
}

func intFromUint64(n uint64) (int, bool) {
	if n > uint64(intMax) {
		return 0, false
	}
	return int(n), true
}

func intFromFloat64(n float64) (int, bool) {
	if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
		return 0, false
	}
	if n < float64(intMin) || n > float64(intMax) {
		return 0, false
	}
	if n < -maxSafeFloatInt || n > maxSafeFloatInt {
		return 0, false
	}
	return int(n), true
}

// GetFloat retrieves the value as a float64, returning def if not found or not a float64.
func (b Bag) GetFloat(key string, def float64) float64 {
	if v, ok := b.Get(key); ok {
		if f, ok := v.(float64); ok {
			return f
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
			return m, true
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
	for k, v := range b {
		merged[k] = v
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
