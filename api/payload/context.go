// Package payload provides abstractions for handling different data formats and conversions.
package payload

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// transcoderCtx is the context key used to store and retrieve the transcoder instance
var transcoderCtx = &ctxapi.Key{Name: "payload.transcoder"}

// GetTranscoder retrieves the Transcoder from the provided context.
// Returns nil if no Transcoder is found in the context.
func GetTranscoder(ctx context.Context) Transcoder {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if t := ac.Get(transcoderCtx); t != nil {
		return t.(Transcoder)
	}
	return nil
}

// WithTranscoder returns a new context with the provided Transcoder attached.
// This allows the Transcoder to be retrieved later using the GetTranscoder function.
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
