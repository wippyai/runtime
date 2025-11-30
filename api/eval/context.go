package eval

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var evalHostKey = &ctxapi.Key{Name: "eval.host"}

// WithHost attaches an eval Host to the application context.
func WithHost(ctx context.Context, host Host) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(evalHostKey) == nil {
		ac.With(evalHostKey, host)
	}
	return ctx
}

// GetHost retrieves the eval Host from the context.
func GetHost(ctx context.Context) Host {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if h := ac.Get(evalHostKey); h != nil {
		return h.(Host)
	}
	return nil
}
