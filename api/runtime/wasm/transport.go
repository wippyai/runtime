package wasm

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime/resource"
)

// Transport type constants for WASM function invocation.
const (
	TransportPayload  = "payload"   // Default: transcode payloads to canonical ABI
	TransportWASIHTTP = "wasi-http" // WASI HTTP: pass request/response handles
)

var transportRegistryKey = &ctxapi.Key{Name: "wasm.transports"}

// Transport prepares WASM function arguments from context and input payloads.
// Implementations are stateless - a single instance is shared across all processes.
type Transport interface {
	// Name returns the transport identifier used in config.
	Name() string

	// Prepare converts context and payloads into raw uint64 arguments for WASM.
	// It receives the process's resource store for handle creation.
	// The args slice is pre-allocated and should be appended to.
	Prepare(ctx context.Context, store *resource.Store, input payload.Payloads, args []uint64) ([]uint64, error)
}

// TransportRegistry holds registered transports by name.
type TransportRegistry interface {
	Register(t Transport)
	Get(name string) Transport
}

// defaultRegistry is a map-based registry.
// Not thread-safe: Register during boot only, Get is safe for concurrent reads.
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

// WithTransportRegistry stores a TransportRegistry in the AppContext.
func WithTransportRegistry(ctx context.Context, reg TransportRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(transportRegistryKey) == nil {
		ac.With(transportRegistryKey, reg)
	}
	return ctx
}

// GetTransportRegistry retrieves the TransportRegistry from the AppContext.
func GetTransportRegistry(ctx context.Context) TransportRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if reg := ac.Get(transportRegistryKey); reg != nil {
		return reg.(TransportRegistry)
	}
	return nil
}
