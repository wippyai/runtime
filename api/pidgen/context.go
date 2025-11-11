package pidgen

import (
	"context"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/internal/uniqid"
)

// Context key for storing PID generator
var generatorCtx = &ctxapi.Key{Name: "pidgen.generator", Scope: ctxapi.ScopeThread}

// WithGenerator attaches a PID generator to the context (app-level service)
func WithGenerator(ctx context.Context, gen *uniqid.PIDGenerator) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(generatorCtx) == nil {
		ac.With(generatorCtx, gen)
	}
	return ctx
}

// GetGenerator retrieves the PID generator from the context
// Returns nil if no generator is found
func GetGenerator(ctx context.Context) *uniqid.PIDGenerator {
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
