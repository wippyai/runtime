package context

import "sync"

// Values stores arbitrary key-value pairs for carrying data between calls.
// Thread-safe replacement for Contexter[any] without generics.
type Values struct {
	mu     sync.RWMutex
	values map[any]any
}

// NewValues creates a new Values instance.
func NewValues() *Values {
	return &Values{
		values: make(map[any]any),
	}
}

// Set stores a value by key.
func (v *Values) Set(key any, value any) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.values[key] = value
}

// Get retrieves a value by key. Returns nil if not found.
func (v *Values) Get(key any) any {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.values[key]
}

// Iterate calls fn for each key-value pair.
func (v *Values) Iterate(fn func(key any, value any)) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	for k, val := range v.values {
		fn(k, val)
	}
}

// Len returns the number of stored values.
func (v *Values) Len() int {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return len(v.values)
}

// Clone creates a copy of this Values with same key-value pairs.
func (v *Values) Clone() *Values {
	v.mu.RLock()
	defer v.mu.RUnlock()

	clone := NewValues()
	for k, val := range v.values {
		clone.values[k] = val
	}
	return clone
}
