package temporal

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
)

// Context keys for storing temporal-related data
var (
	clientInterceptorRegistryKey = &ctxapi.Key{Name: "temporal.client.interceptor.registry"}
	workerInterceptorRegistryKey = &ctxapi.Key{Name: "temporal.worker.interceptor.registry"}
	dataConverterRegistryKey     = &ctxapi.Key{Name: "temporal.dataconverter.registry"}
	activityContextKey           = &ctxapi.Key{Name: "temporal.activity.context"}
)

// ClientInterceptorRegistry provides access to registered client interceptors
type ClientInterceptorRegistry interface {
	// Register adds a client interceptor
	Register(interceptor interceptor.ClientInterceptor)
	// GetAll returns all registered client interceptors
	GetAll() []interceptor.ClientInterceptor
}

// WorkerInterceptorRegistry provides access to registered worker interceptors
type WorkerInterceptorRegistry interface {
	// Register adds a worker interceptor
	Register(interceptor interceptor.WorkerInterceptor)
	// GetAll returns all registered worker interceptors
	GetAll() []interceptor.WorkerInterceptor
}

// DataConverterRegistry provides access to data converter configuration
type DataConverterRegistry interface {
	// RegisterCodec adds a custom payload codec
	RegisterCodec(codec converter.PayloadCodec)
	// Build creates the final data converter with all registered codecs
	Build() converter.DataConverter
}

// WithClientInterceptorRegistry attaches a ClientInterceptorRegistry instance to the provided context.
func WithClientInterceptorRegistry(ctx context.Context, registry ClientInterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(clientInterceptorRegistryKey) == nil {
		ac.With(clientInterceptorRegistryKey, registry)
	}
	return ctx
}

// GetClientInterceptorRegistry retrieves the ClientInterceptorRegistry instance from the provided context.
// Returns nil if no ClientInterceptorRegistry is found in the context.
func GetClientInterceptorRegistry(ctx context.Context) ClientInterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if registry := ac.Get(clientInterceptorRegistryKey); registry != nil {
		return registry.(ClientInterceptorRegistry)
	}
	return nil
}

// WithWorkerInterceptorRegistry attaches a WorkerInterceptorRegistry instance to the provided context.
func WithWorkerInterceptorRegistry(ctx context.Context, registry WorkerInterceptorRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(workerInterceptorRegistryKey) == nil {
		ac.With(workerInterceptorRegistryKey, registry)
	}
	return ctx
}

// GetWorkerInterceptorRegistry retrieves the WorkerInterceptorRegistry instance from the provided context.
// Returns nil if no WorkerInterceptorRegistry is found in the context.
func GetWorkerInterceptorRegistry(ctx context.Context) WorkerInterceptorRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if registry := ac.Get(workerInterceptorRegistryKey); registry != nil {
		return registry.(WorkerInterceptorRegistry)
	}
	return nil
}

// WithDataConverterRegistry attaches a DataConverterRegistry instance to the provided context.
func WithDataConverterRegistry(ctx context.Context, registry DataConverterRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(dataConverterRegistryKey) == nil {
		ac.With(dataConverterRegistryKey, registry)
	}
	return ctx
}

// GetDataConverterRegistry retrieves the DataConverterRegistry instance from the provided context.
// Returns nil if no DataConverterRegistry is found in the context.
func GetDataConverterRegistry(ctx context.Context) DataConverterRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if registry := ac.Get(dataConverterRegistryKey); registry != nil {
		return registry.(DataConverterRegistry)
	}
	return nil
}

// ActivityContextKey returns the context key for activity context storage
func ActivityContextKey() *ctxapi.Key {
	return activityContextKey
}

// WithActivityContext attaches a Temporal activity context to the provided context.
func WithActivityContext(ctx context.Context, activityCtx context.Context) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(activityContextKey) == nil {
		ac.With(activityContextKey, activityCtx)
	}
	return ctx
}

// GetActivityContext retrieves the Temporal activity context from the provided context.
// Returns nil if no activity context is found in the context.
func GetActivityContext(ctx context.Context) context.Context {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	if activityCtx, ok := fc.Get(activityContextKey); ok && activityCtx != nil {
		return activityCtx.(context.Context)
	}
	return nil
}
