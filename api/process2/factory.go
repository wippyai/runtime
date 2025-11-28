package process2

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
)

// Event system and kinds for factory registration.
const (
	FactorySystem   event.System = "process2.factory"
	FactoryRegister event.Kind   = "factory.register"
	FactoryDelete   event.Kind   = "factory.delete"
)

// Factory creates Process instances from registry IDs.
// Providers (Lua, WASM) register factories, consumers get processes by ID.
type Factory interface {
	Create(id registry.ID) (Process, error)
}

// FactoryEntry is sent via event bus to register a factory.
type FactoryEntry struct {
	Factory ProcessFactory
}

// context key for factory
var factoryKey = &ctxapi.Key{Name: "process2.factory", Inherit: true}

// GetFactory retrieves the factory from context.
func GetFactory(ctx context.Context) Factory {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if val, ok := fc.Get(factoryKey); ok {
		if f, ok := val.(Factory); ok {
			return f
		}
	}
	return nil
}

// WithFactory stores a factory in the frame context.
func WithFactory(ctx context.Context, f Factory) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return ctxapi.ErrNoFrameContext
	}
	return fc.Set(factoryKey, f)
}
