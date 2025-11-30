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

// Registrar allows registering handlers during boot.
// Separate from Registry to distinguish read vs write operations.
type Registrar interface {
	Registry
	Register(id CommandID, h Handler)
}

// Freezer allows freezing the registry after boot.
// After freeze, the registry becomes immutable and lock-free.
type Freezer interface {
	Freeze()
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

// GetRegistrar retrieves the dispatcher registrar from context.
// Returns nil if no registrar is set or if it doesn't support registration.
func GetRegistrar(ctx context.Context) Registrar {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(registryKey); ok {
		if r, ok := val.(Registrar); ok {
			return r
		}
	}
	return nil
}

// GetDispatcher retrieves the dispatcher from context.
// Returns nil if no dispatcher is set.
func GetDispatcher(ctx context.Context) Dispatcher {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(registryKey); ok {
		if d, ok := val.(Dispatcher); ok {
			return d
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
