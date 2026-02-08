package payload

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var transcoderCtx = &ctxapi.Key{Name: "payload.transcoder"}

// WithTranscoder attaches a Transcoder to the context.
func WithTranscoder(ctx context.Context, transcoder Transcoder) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(transcoderCtx) == nil {
		ac.With(transcoderCtx, transcoder)
	}
	return ctx
}

// GetTranscoder retrieves the Transcoder from the context.
func GetTranscoder(ctx context.Context) Transcoder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if t := ac.Get(transcoderCtx); t != nil {
		tc := t.(Transcoder)
		return tc
	}
	return nil
}
