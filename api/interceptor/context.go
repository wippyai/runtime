package interceptor

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing interceptor-related data
var (
	registryCtx = &ctxapi.Key{Name: "interceptor.registry", Scope: ctxapi.ScopeThread}
	optionsCtx  = &ctxapi.Key{Name: "interceptor.options", Scope: ctxapi.ScopeThread}
	cancelCtx   = &ctxapi.Key{Name: "interceptor.cancel", Scope: ctxapi.ScopeCall}
)

// WithInterceptor adds the interceptor registry to the context
func WithInterceptor(ctx context.Context, registry Registry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(registryCtx) == nil {
		ac.With(registryCtx, registry)
	}
	return ctx
}

// GetInterceptors retrieves the interceptor registry from the context
func GetInterceptors(ctx context.Context) Registry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(registryCtx); val != nil {
		if registry, ok := val.(Registry); ok {
			return registry
		}
	}
	return nil
}

func GetOptions(ctx context.Context) Options {
	if options, ok := ctx.Value(optionsCtx).(Options); ok {
		return options
	}

	return Options{}
}

func WithOptions(ctx context.Context, options Options) context.Context {
	return context.WithValue(ctx, optionsCtx, options)
}

func WithCancel(ctx context.Context, cancel context.CancelFunc) context.Context {
	return context.WithValue(ctx, cancelCtx, cancel)
}

func GetCancel(ctx context.Context) context.CancelFunc {
	if cancel, ok := ctx.Value(cancelCtx).(context.CancelFunc); ok {
		return cancel
	}
	return nil
}
