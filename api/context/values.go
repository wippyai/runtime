// Package context provides application-level context management utilities.
package context

import (
	stdcontext "context"

	"github.com/wippyai/runtime/api/attrs"
)

// valuesKey is the context key for storing Values or Contexter[any]
// Marked as inheritable so values automatically flow through process/function boundaries
var valuesKey = &Key{Name: "values", Inherit: true}

// ValuesCtx is the public accessor for the values context key
var ValuesCtx = valuesKey

// ValuesPair creates a context.Pair for setting custom values.
// Use this to build context override pairs for Task/Launch.
func ValuesPair(values Values) Pair {
	return Pair{Key: valuesKey, Value: values}
}

// SetValues stores a Values in the FrameContext.
// Returns error if no frame context or frame is sealed.
func SetValues(ctx stdcontext.Context, values Values) error {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(ValuesCtx, values)
}

// GetValues retrieves the Values from FrameContext.
// Returns nil if not found or wrong type.
func GetValues(ctx stdcontext.Context) Values {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	val, ok := fc.Get(ValuesCtx)
	if !ok {
		return nil
	}
	values, ok := val.(Values)
	if !ok {
		return nil
	}
	return values
}

// GetOrCreateValues retrieves existing Values or creates a new one.
// If parent has Values and is sealed, this clones them for the new frame.
// Returns the Values instance and an error if frame context is not available.
func GetOrCreateValues(ctx stdcontext.Context) (Values, error) {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	// Try to get existing values
	val, ok := fc.Get(ValuesCtx)
	if ok {
		if values, ok := val.(Values); ok {
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

// Values is an alias to attrs.Bag for storing arbitrary key-value pairs.
// Used for carrying data between calls in frame context.
type Values = attrs.Bag

// NewValues creates a new Values instance.
func NewValues() Values {
	return attrs.NewBag()
}
