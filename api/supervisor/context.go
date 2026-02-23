// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var supervisorKey = &ctxapi.Key{Name: "supervisor"}

// GetSupervisor retrieves the supervisor from the context.
// Returns the supervisor as any to avoid import cycles.
// Callers should type-assert to *supervisor.Supervisor from system/supervisor package.
func GetSupervisor(ctx context.Context) any {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	return ac.Get(supervisorKey)
}

// WithSupervisor stores the supervisor in the context.
func WithSupervisor(ctx context.Context, supervisor any) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(supervisorKey) == nil {
		ac.With(supervisorKey, supervisor)
	}
	return ctx
}
