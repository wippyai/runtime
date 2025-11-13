// Package interceptor provides request and operation interception.
package interceptor

import "time"

// Bag is a concrete implementation of the Options interface
type Bag struct {
	data map[string]any
}

// NewBag creates a new options bag
func NewBag() *Bag {
	return &Bag{
		data: make(map[string]any),
	}
}

// NewBagFrom creates a new options bag from existing data
func NewBagFrom(data map[string]any) *Bag {
	b := &Bag{
		data: make(map[string]any, len(data)),
	}
	for k, v := range data {
		b.data[k] = v
	}
	return b
}

// Set sets a value in the bag
func (b *Bag) Set(key string, value any) {
	b.data[key] = value
}

// Get retrieves a value from the bag
func (b *Bag) Get(key string) (any, bool) {
	val, ok := b.data[key]
	return val, ok
}

// GetString retrieves a string value with a default
func (b *Bag) GetString(key string, def string) string {
	if val, ok := b.data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return def
}

// GetInt retrieves an int value with a default
func (b *Bag) GetInt(key string, def int) int {
	if val, ok := b.data[key]; ok {
		if i, ok := val.(int); ok {
			return i
		}
	}
	return def
}

// GetBool retrieves a bool value with a default
func (b *Bag) GetBool(key string, def bool) bool {
	if val, ok := b.data[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return def
}

// GetDuration retrieves a duration value with a default
func (b *Bag) GetDuration(key string, def time.Duration) time.Duration {
	if val, ok := b.data[key]; ok {
		if d, ok := val.(time.Duration); ok {
			return d
		}
	}
	return def
}

// Merge merges another Options bag into this one, returning a new bag
func (b *Bag) Merge(other Options) Options {
	result := NewBag()

	// Copy current data
	for k, v := range b.data {
		result.data[k] = v
	}

	// Merge other bag
	if otherBag, ok := other.(*Bag); ok {
		for k, v := range otherBag.data {
			result.data[k] = v
		}
	}

	return result
}

// Clone creates a deep copy of the bag
func (b *Bag) Clone() Options {
	result := NewBag()
	for k, v := range b.data {
		result.data[k] = v
	}
	return result
}
