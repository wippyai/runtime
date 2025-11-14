// Package interceptor provides request and operation interception.
package interceptor

import (
	"context"
	"errors"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing interceptor-related data
var (
	chainCtx   = &ctxapi.Key{Name: "interceptor.chain"}
	optionsCtx = &ctxapi.Key{Name: "interceptor.options"}
)

// WithChain adds the interceptor chain to the context
func WithChain(ctx context.Context, chain Chain) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(chainCtx) == nil {
		ac.With(chainCtx, chain)
	}
	return ctx
}

// GetChain retrieves the interceptor chain from the context
func GetChain(ctx context.Context) Chain {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(chainCtx); val != nil {
		if chain, ok := val.(Chain); ok {
			return chain
		}
	}
	return nil
}

// SetOptions sets interceptor options in the FrameContext
func SetOptions(ctx context.Context, options Options) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return errors.New("no frame context available")
	}
	return fc.Set(optionsCtx, options)
}

// GetOptions retrieves interceptor options from the FrameContext
func GetOptions(ctx context.Context) (Options, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(optionsCtx); ok {
		if opts, ok := val.(Options); ok {
			return opts, true
		}
	}
	return nil, false
}
