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

var linksKey = &ctxapi.Key{Name: "cluster.links"}

// WithLinks attaches the node's peer links to the app context.
func WithLinks(ctx context.Context, links Links) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(linksKey) == nil {
		ac.With(linksKey, links)
	}
	return ctx
}

// GetLinks retrieves the node's peer links from the app context.
func GetLinks(ctx context.Context) Links {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	links, _ := ac.Get(linksKey).(Links)
	return links
}
