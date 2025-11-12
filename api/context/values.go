package context

import (
	stdcontext "context"
	"sync"
)

// valuesKey is the context key for storing Values or Contexter[any]
// Marked as inheritable so values automatically flow through process/function boundaries
var valuesKey = &Key{Name: "values", Inherit: true}

// ValuesCtx is the public accessor for the values context key
var ValuesCtx = valuesKey

// ValuesPair creates a context.Pair for setting custom values.
// Use this to build context override pairs for Task/Launch.
func ValuesPair(values *Values) Pair {
	return Pair{Key: valuesKey, Value: values}
}

// SetValues stores a Values in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetValues(ctx stdcontext.Context, values *Values) error {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(ValuesCtx, values)
}

// GetValues retrieves the Values from FrameContext.
// Returns nil if not found or wrong type.
func GetValues(ctx stdcontext.Context) *Values {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	val, ok := fc.Get(ValuesCtx)
	if !ok {
		return nil
	}
	values, ok := val.(*Values)
	if !ok {
		return nil
	}
	return values
}

// GetOrCreateValues retrieves existing Values or creates a new one.
// If parent has Values and is sealed, this clones them for the new frame.
// Returns the Values instance and an error if frame context is not available.
func GetOrCreateValues(ctx stdcontext.Context) (*Values, error) {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	// Try to get existing values
	val, ok := fc.Get(ValuesCtx)
	if ok {
		if values, ok := val.(*Values); ok {
			return values, nil
		}
	}

	// No values yet, create new one
	values := NewValues()
	if err := fc.Set(ValuesCtx, values); err != nil {
		return nil, err
	}
	return values, nil
}

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
