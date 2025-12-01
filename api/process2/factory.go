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
	FactoryAccept   event.Kind   = "factory.accept"
	FactoryReject   event.Kind   = "factory.reject"
)

// ProcessMeta contains metadata about a process type.
// Returned alongside Process from Factory.Create().
type ProcessMeta struct {
	Method string // Default entry point method (e.g., "main", "handle")
}

// Factory creates Process instances from registry IDs.
// Providers (Lua, WASM) register factories, consumers get processes by ID.
type Factory interface {
	Create(id registry.ID) (Process, *ProcessMeta, error)
}

// FactoryEntry is sent via event bus to register a factory.
type FactoryEntry struct {
	Factory ProcessFactory
	Meta    ProcessMeta // Metadata for all processes from this factory
}

// context key for factory
var factoryKey = &ctxapi.Key{Name: "process2.factory"}

// GetFactory retrieves the factory from context.
func GetFactory(ctx context.Context) Factory {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(factoryKey); val != nil {
		if f, ok := val.(Factory); ok {
			return f
		}
	}
	return nil
}

// WithFactory stores a factory in the app context.
func WithFactory(ctx context.Context, f Factory) {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return
	}
	ac.With(factoryKey, f)
}
