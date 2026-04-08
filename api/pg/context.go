// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var pgKey = &ctxapi.Key{Name: "pg"}

// WithProcessGroups attaches a ProcessGroups implementation to the context.
func WithProcessGroups(ctx context.Context, pg ProcessGroups) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(pgKey) == nil {
		ac.With(pgKey, pg)
	}
	return ctx
}

// GetProcessGroups retrieves the ProcessGroups from the context.
func GetProcessGroups(ctx context.Context) ProcessGroups {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(pgKey); val != nil {
		if pg, ok := val.(ProcessGroups); ok {
			return pg
		}
	}
	return nil
}
