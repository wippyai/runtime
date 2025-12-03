package wasm

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime/resource"
)

var transportRegistryCtx = &ctxapi.Key{Name: "wasm.transports"}

// Transport prepares WASM function arguments from context and input payloads.
// Implementations are stateless - a single instance is shared across all processes.
// The Prepare method must be fast and allocation-minimal.
type Transport interface {
	// Name returns the transport identifier used in config.
	Name() string

	// Prepare converts context and payloads into raw uint64 arguments for WASM.
	// It receives the process's resource store for handle creation.
	// The args slice is pre-allocated and should be appended to.
	// Returns the arguments to pass to the WASM export.
	Prepare(ctx context.Context, store *resource.Store, input payload.Payloads, args []uint64) ([]uint64, error)
}

// TransportRegistry holds registered transports by name.
type TransportRegistry interface {
	// Register adds a transport to the registry.
	Register(t Transport)

	// Get returns a transport by name, or nil if not found.
	Get(name string) Transport
}

// defaultRegistry is a simple map-based registry.
type defaultRegistry struct {
	transports map[string]Transport
}

// NewTransportRegistry creates a new transport registry.
func NewTransportRegistry() TransportRegistry {
	return &defaultRegistry{
		transports: make(map[string]Transport),
	}
}

func (r *defaultRegistry) Register(t Transport) {
	r.transports[t.Name()] = t
}

func (r *defaultRegistry) Get(name string) Transport {
	return r.transports[name]
}

// WithTransportRegistry returns a new context with the provided TransportRegistry attached.
func WithTransportRegistry(ctx context.Context, reg TransportRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(transportRegistryCtx) == nil {
		ac.With(transportRegistryCtx, reg)
	}
	return ctx
}

// GetTransportRegistry retrieves the TransportRegistry from the provided context.
func GetTransportRegistry(ctx context.Context) TransportRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(transportRegistryCtx); reg != nil {
		return reg.(TransportRegistry)
	}
	return nil
}
