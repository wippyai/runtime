// Package otel provides OpenTelemetry service integration.
package otel

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.opentelemetry.io/otel/trace"
)

// Context keys for storing otel-related data
var (
	tracerCtx      = &ctxapi.Key{Name: "otel.tracer"}
	spanCtx        = &ctxapi.Key{Name: "otel.span", Inherit: true}
	spanContextKey = &ctxapi.Key{Name: "otel.spancontext", Inherit: true}
)

// WithTracer adds the tracer to the AppContext
func WithTracer(ctx context.Context, tracer trace.Tracer) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(tracerCtx) == nil {
		ac.With(tracerCtx, tracer)
	}
	return ctx
}

// GetTracer retrieves the tracer from the AppContext
func GetTracer(ctx context.Context) trace.Tracer {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(tracerCtx); val != nil {
		if tracer, ok := val.(trace.Tracer); ok {
			return tracer
		}
	}
	return nil
}

// SetSpan sets the current span in the FrameContext
func SetSpan(ctx context.Context, span trace.Span) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(spanCtx, span)
}

// GetSpan retrieves the current span from the FrameContext
func GetSpan(ctx context.Context) (trace.Span, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(spanCtx); ok {
		if span, ok := val.(trace.Span); ok {
			return span, true
		}
	}
	return nil, false
}

// GetSpanKey returns the context key for storing spans
func GetSpanKey() *ctxapi.Key {
	return spanCtx
}

// SetRemoteSpanContext stores a SpanContext for trace propagation without an active span
func SetRemoteSpanContext(ctx context.Context, sc trace.SpanContext) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(spanContextKey, sc)
}

// GetRemoteSpanContext retrieves a stored SpanContext
func GetRemoteSpanContext(ctx context.Context) (trace.SpanContext, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return trace.SpanContext{}, false
	}
	if val, ok := fc.Get(spanContextKey); ok {
		if sc, ok := val.(trace.SpanContext); ok {
			return sc, true
		}
	}
	return trace.SpanContext{}, false
}

// GetRemoteSpanContextKey returns the context key for storing remote span contexts
func GetRemoteSpanContextKey() *ctxapi.Key {
	return spanContextKey
}
