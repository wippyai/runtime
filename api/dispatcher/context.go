package dispatcher

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// registryKey is the context key for the dispatcher registry.
var registryKey = &ctxapi.Key{Name: "dispatcher.registry", Inherit: true}

// Registry provides O(1) command-to-handler lookup.
type Registry interface {
	Get(id CommandID) Handler
	Has(id CommandID) bool
}

// GetRegistry retrieves the dispatcher registry from context.
// Returns nil if no registry is set.
func GetRegistry(ctx context.Context) Registry {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(registryKey); ok {
		if r, ok := val.(Registry); ok {
			return r
		}
	}
	return nil
}

// WithRegistry stores a dispatcher registry in the frame context.
// Returns error if no frame context or frame is sealed.
func WithRegistry(ctx context.Context, r Registry) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(registryKey, r)
}
