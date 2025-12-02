package process

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// registryKey is the context key for the dispatcher registry in AppContext.
var registryKey = &ctxapi.Key{Name: "dispatcher.registry"}

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

// GetRegistry retrieves the dispatcher registry from AppContext.
// Returns nil if no registry is set.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r, ok := ac.Get(registryKey).(Registry); ok {
		return r
	}
	return nil
}

// GetRegistrar retrieves the dispatcher registrar from AppContext.
// Returns nil if no registrar is set or if it doesn't support registration.
func GetRegistrar(ctx context.Context) Registrar {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if r, ok := ac.Get(registryKey).(Registrar); ok {
		return r
	}
	return nil
}

// GetDispatcher retrieves the dispatcher from AppContext.
// Returns nil if no dispatcher is set.
func GetDispatcher(ctx context.Context) Dispatcher {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if d, ok := ac.Get(registryKey).(Dispatcher); ok {
		return d
	}
	return nil
}

// WithRegistry stores a dispatcher registry in the AppContext.
// Returns error if no AppContext is available.
func WithRegistry(ctx context.Context, r Registry) error {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctxapi.ErrNoAppContext
	}
	ac.With(registryKey, r)
	return nil
}
