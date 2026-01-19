package process

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var (
	managerKey           = &ctxapi.Key{Name: "process.manager"}
	factoryKey           = &ctxapi.Key{Name: "process.factory"}
	generatorKey         = &ctxapi.Key{Name: "pidgen.generator"}
	lifecycleRegistryKey = &ctxapi.Key{Name: "process.lifecycle_registry"}
)

// WithManager attaches a process Manager to the context.
func WithManager(ctx context.Context, m Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerKey) == nil {
		ac.With(managerKey, m)
	}
	return ctx
}

// GetManager retrieves the process Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerKey); val != nil {
		if m, ok := val.(Manager); ok {
			return m
		}
	}
	return nil
}

// WithFactory stores a factory in the app context.
func WithFactory(ctx context.Context, f Factory) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(factoryKey) == nil {
		ac.With(factoryKey, f)
	}
	return ctx
}

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

// WithPIDGenerator attaches a PID generator to the context.
func WithPIDGenerator(ctx context.Context, gen PIDGenerator) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(generatorKey) == nil {
		ac.With(generatorKey, gen)
	}
	return ctx
}

// GetPIDGenerator retrieves the PID generator from the context.
func GetPIDGenerator(ctx context.Context) PIDGenerator {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(generatorKey); val != nil {
		if gen, ok := val.(PIDGenerator); ok {
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
	if ac.Get(lifecycleRegistryKey) == nil {
		ac.With(lifecycleRegistryKey, reg)
	}
	return ctx
}

// GetLifecycleRegistry retrieves the lifecycle registry from the context.
func GetLifecycleRegistry(ctx context.Context) LifecycleRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(lifecycleRegistryKey); val != nil {
		if reg, ok := val.(LifecycleRegistry); ok {
			return reg
		}
	}
	return nil
}
