package contract

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var contractsKey = &ctxapi.Key{Name: "contracts"}

// contractServices holds both registry and instantiator for context storage.
type contractServices struct {
	Registry     Registry
	Instantiator Instantiator
}

// WithContracts attaches both contract Registry and Instantiator to context.
func WithContracts(ctx context.Context, registry Registry, instantiator Instantiator) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(contractsKey) == nil {
		services := &contractServices{
			Registry:     registry,
			Instantiator: instantiator,
		}
		ac.With(contractsKey, services)
	}
	return ctx
}

// GetRegistry retrieves the contract registry from the provided context.
func GetRegistry(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(contractsKey); val != nil {
		if services, ok := val.(*contractServices); ok {
			return services.Registry
		}
	}
	return nil
}

// GetInstantiator retrieves the contract instantiator from the provided context.
func GetInstantiator(ctx context.Context) Instantiator {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(contractsKey); val != nil {
		if services, ok := val.(*contractServices); ok {
			return services.Instantiator
		}
	}
	return nil
}
