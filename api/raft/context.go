// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var serviceKey = &ctxapi.Key{Name: "raft.service"}

// WithService attaches a Raft Service to the provided context.
func WithService(ctx context.Context, svc Service) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(serviceKey) == nil {
		ac.With(serviceKey, svc)
	}
	return ctx
}

// GetService retrieves the Raft Service from the provided context.
// Returns nil if no Raft service is found.
func GetService(ctx context.Context) Service {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(serviceKey); val != nil {
		if svc, ok := val.(Service); ok {
			return svc
		}
	}
	return nil
}
