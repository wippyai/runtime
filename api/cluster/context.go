// SPDX-License-Identifier: MPL-2.0

package cluster

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var membershipKey = &ctxapi.Key{Name: "cluster.membership"}

// WithMembership attaches cluster membership to the app context.
func WithMembership(ctx context.Context, membership Membership) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(membershipKey) == nil {
		ac.With(membershipKey, membership)
	}
	return ctx
}

// GetMembership retrieves cluster membership from the app context.
func GetMembership(ctx context.Context) Membership {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(membershipKey); val != nil {
		if membership, ok := val.(Membership); ok {
			return membership
		}
	}
	return nil
}
