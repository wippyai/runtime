package contract

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

var contractsCtx = &ctxapi.Key{Name: "contracts"}

// contractServices holds both registry and instantiator for context storage
type contractServices struct {
	Registry     Registry
	Instantiator Instantiator
}

// WithServices returns a new context with both contract Registry and Instantiator attached.
// This allows both to be retrieved later using the getter functions.
func WithServices(ctx context.Context, registry Registry, instantiator Instantiator) context.Context {
	services := &contractServices{
		Registry:     registry,
		Instantiator: instantiator,
	}
	return context.WithValue(ctx, contractsCtx, services)
}

// GetRegistry retrieves the contract registry from the provided context.
// Returns nil if no Registry is found in the context.
func GetRegistry(ctx context.Context) Registry {
	if services, ok := ctx.Value(contractsCtx).(*contractServices); ok {
		return services.Registry
	}
	return nil
}

// GetInstantiator retrieves the contract instantiator from the provided context.
// Returns nil if no Instantiator is found in the context.
func GetInstantiator(ctx context.Context) Instantiator {
	if services, ok := ctx.Value(contractsCtx).(*contractServices); ok {
		return services.Instantiator
	}
	return nil
}
