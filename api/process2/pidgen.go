package process2

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/internal/uniqid"
)

var generatorCtx = &ctxapi.Key{Name: "pidgen.generator"}

// WithPIDGenerator attaches a PID generator to the context (app-level service).
func WithPIDGenerator(ctx context.Context, gen *uniqid.PIDGenerator) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(generatorCtx) == nil {
		ac.With(generatorCtx, gen)
	}
	return ctx
}

// GetPIDGenerator retrieves the PID generator from the context.
// Returns nil if no generator is found.
func GetPIDGenerator(ctx context.Context) *uniqid.PIDGenerator {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(generatorCtx); val != nil {
		if gen, ok := val.(*uniqid.PIDGenerator); ok {
			return gen
		}
	}
	return nil
}
