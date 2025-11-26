package metrics

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
)

var collectorKey = &ctxapi.Key{Name: "metrics.collector"}

func SetCollector(ctx context.Context, c Collector) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(collectorKey) == nil {
		ac.With(collectorKey, c)
	}
	return ctx
}

func GetCollector(ctx context.Context) Collector {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if c, ok := ac.Get(collectorKey).(Collector); ok {
		return c
	}
	return nil
}
