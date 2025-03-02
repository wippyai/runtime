// Package payload provides abstractions for handling different data formats and conversions.
package payload

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
)

// transcoderCtx is the context key used to store and retrieve the transcoder instance
var transcoderCtx = &ctxapi.Key{Name: "payload.transcoder"} //nolint:gochecknoglobals

// GetTranscoder retrieves the Transcoder from the provided context.
// Returns nil if no Transcoder is found in the context.
func GetTranscoder(ctx context.Context) Transcoder {
	if t, ok := ctx.Value(transcoderCtx).(Transcoder); ok {
		return t
	}
	return nil
}

// WithTranscoder returns a new context with the provided Transcoder attached.
// This allows the Transcoder to be retrieved later using the GetTranscoder function.
func WithTranscoder(ctx context.Context, transcoder Transcoder) context.Context {
	return context.WithValue(ctx, transcoderCtx, transcoder)
}
