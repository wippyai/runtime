package temporal

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.temporal.io/sdk/interceptor"
)

// Context keys for storing temporal-related data
var (
	activityContextKey      = &ctxapi.Key{Name: "temporal.activity.context"}
	clientIDKey             = &ctxapi.Key{Name: "temporal.client.id"}
	clientInterceptorRegKey = &ctxapi.Key{Name: "temporal.interceptor.client"}
	workerInterceptorRegKey = &ctxapi.Key{Name: "temporal.interceptor.worker"}
)

// ClientInterceptorRegistry provides methods to register client interceptors
type ClientInterceptorRegistry interface {
	Register(i interceptor.ClientInterceptor)
	GetAll() []interceptor.ClientInterceptor
}

// WorkerInterceptorRegistry provides methods to register worker interceptors
type WorkerInterceptorRegistry interface {
	Register(i interceptor.WorkerInterceptor)
	GetAll() []interceptor.WorkerInterceptor
}

// WithClientInterceptorRegistry stores the client interceptor registry in context
func WithClientInterceptorRegistry(ctx context.Context, reg ClientInterceptorRegistry) context.Context {
	return context.WithValue(ctx, clientInterceptorRegKey, reg)
}

// GetClientInterceptorRegistry retrieves the client interceptor registry from context
func GetClientInterceptorRegistry(ctx context.Context) ClientInterceptorRegistry {
	if v := ctx.Value(clientInterceptorRegKey); v != nil {
		if r, ok := v.(ClientInterceptorRegistry); ok {
			return r
		}
	}
	return nil
}

// WithWorkerInterceptorRegistry stores the worker interceptor registry in context
func WithWorkerInterceptorRegistry(ctx context.Context, reg WorkerInterceptorRegistry) context.Context {
	return context.WithValue(ctx, workerInterceptorRegKey, reg)
}

// GetWorkerInterceptorRegistry retrieves the worker interceptor registry from context
func GetWorkerInterceptorRegistry(ctx context.Context) WorkerInterceptorRegistry {
	if v := ctx.Value(workerInterceptorRegKey); v != nil {
		if r, ok := v.(WorkerInterceptorRegistry); ok {
			return r
		}
	}
	return nil
}

// ActivityContextKey returns the context key for activity context storage
func ActivityContextKey() *ctxapi.Key {
	return activityContextKey
}

// WithClientID stores the temporal client ID in context for peer routing
func WithClientID(ctx context.Context, clientID string) context.Context {
	return context.WithValue(ctx, clientIDKey, clientID)
}

// GetClientID retrieves the temporal client ID from context
func GetClientID(ctx context.Context) string {
	if v := ctx.Value(clientIDKey); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
