package metrics

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var collectorCtx = &ctxapi.Key{Name: "metrics.collector"}

// WithCollector attaches a Collector to the context.
func WithCollector(ctx context.Context, c Collector) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(collectorCtx) == nil {
		ac.With(collectorCtx, c)
	}
	return ctx
}

// GetCollector retrieves the Collector from the context.
func GetCollector(ctx context.Context) Collector {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if c, ok := ac.Get(collectorCtx).(Collector); ok {
		return c
	}
	return nil
}
