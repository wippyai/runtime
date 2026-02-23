// SPDX-License-Identifier: MPL-2.0

package context

import "context"

var valuesKey = &Key{Name: "values", Inherit: true}

// ValuesCtx is the public accessor for the values context key.
var ValuesCtx = valuesKey

// ValuesPair creates a context.Pair for setting custom values.
func ValuesPair(values Values) Pair {
	return Pair{Key: valuesKey, Value: values}
}

// SetValues stores Values in the FrameContext.
func SetValues(ctx context.Context, values Values) error {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return ErrNoFrameContext
	}
	return fc.Set(ValuesCtx, values)
}

// GetValues retrieves Values from FrameContext.
func GetValues(ctx context.Context) Values {
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
func GetOrCreateValues(ctx context.Context) (Values, error) {
	fc := FrameFromContext(ctx)
	if fc == nil {
		return nil, ErrNoFrameContext
	}
	val, ok := fc.Get(ValuesCtx)
	if ok {
		if values, ok := val.(Values); ok {
			return values, nil
		}
	}
	values := NewValues()
	if err := fc.Set(ValuesCtx, values); err != nil {
		return nil, err
	}
	return values, nil
}
