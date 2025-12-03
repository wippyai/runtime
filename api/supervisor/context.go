package supervisor

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var supervisorCtxKey = &ctxapi.Key{Name: "supervisor.supervisorCtxKey"}

// GetSupervisor retrieves the supervisor from the context.
// Returns the supervisor as interface{} to avoid import cycles.
// Callers should type-assert to *supervisor.Supervisor from system/supervisor package.
func GetSupervisor(ctx context.Context) interface{} {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	return ac.Get(supervisorCtxKey)
}

// WithSupervisor stores the supervisor in the context.
func WithSupervisor(ctx context.Context, supervisor interface{}) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(supervisorCtxKey) == nil {
		ac.With(supervisorCtxKey, supervisor)
	}
	return ctx
}
