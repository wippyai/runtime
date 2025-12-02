package process2

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var managerCtx = &ctxapi.Key{Name: "process2.manager"}

// WithManager attaches a process Manager to the context.
func WithManager(ctx context.Context, m Manager) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(managerCtx) == nil {
		ac.With(managerCtx, m)
	}
	return ctx
}

// GetManager retrieves the process Manager from the context.
func GetManager(ctx context.Context) Manager {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(managerCtx); val != nil {
		if m, ok := val.(Manager); ok {
			return m
		}
	}
	return nil
}
