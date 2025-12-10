package process

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/internal/uniqid"
)

var (
	managerCtxKey           = &ctxapi.Key{Name: "process.manager"}
	factoryCtxKey           = &ctxapi.Key{Name: "process.factory"}
	generatorCtxKey         = &ctxapi.Key{Name: "pidgen.generator"}
	lifecycleRegistryCtxKey = &ctxapi.Key{Name: "process.lifecycle_registry"}
)

// WithManager attaches a process Manager to the context.
func WithManager(ctx context.Context, m Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerCtxKey) == nil {
		ac.With(managerCtxKey, m)
	}
	return ctx
}

// GetManager retrieves the process Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerCtxKey); val != nil {
		if m, ok := val.(Manager); ok {
			return m
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
	ac.With(factoryCtxKey, f)
}

// GetFactory retrieves the factory from context.
func GetFactory(ctx context.Context) Factory {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(factoryCtxKey); val != nil {
		if f, ok := val.(Factory); ok {
			return f
		}
	}
	return nil
}

// WithPIDGenerator attaches a PID generator to the context.
func WithPIDGenerator(ctx context.Context, gen *uniqid.PIDGenerator) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(generatorCtxKey) == nil {
		ac.With(generatorCtxKey, gen)
	}
	return ctx
}

// GetPIDGenerator retrieves the PID generator from the context.
func GetPIDGenerator(ctx context.Context) *uniqid.PIDGenerator {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(generatorCtxKey); val != nil {
		if gen, ok := val.(*uniqid.PIDGenerator); ok {
			return gen
		}
	}
	return nil
}

// WithLifecycleRegistry attaches a lifecycle registry to the context.
func WithLifecycleRegistry(ctx context.Context, reg LifecycleRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(lifecycleRegistryCtxKey) == nil {
		ac.With(lifecycleRegistryCtxKey, reg)
	}
	return ctx
}

// GetLifecycleRegistry retrieves the lifecycle registry from the context.
func GetLifecycleRegistry(ctx context.Context) LifecycleRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(lifecycleRegistryCtxKey); val != nil {
		if reg, ok := val.(LifecycleRegistry); ok {
			return reg
		}
	}
	return nil
}
