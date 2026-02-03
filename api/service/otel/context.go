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

// propagatableSpan wraps a trace.Span and implements ctxapi.Propagator
// to automatically convert to SpanContext during cross-process propagation.
type propagatableSpan struct {
	trace.Span
}

// PropagateValue implements ctxapi.Propagator.
// Returns the SpanContext for cross-process propagation, or nil if invalid.
func (p *propagatableSpan) PropagateValue() any {
	if p.Span == nil {
		return nil
	}
	sc := p.SpanContext()
	if !sc.IsValid() {
		return nil
	}
	return sc
}

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

// SetSpan sets the current span in the FrameContext.
// The span is wrapped to support automatic conversion to SpanContext
// during cross-process propagation via ctxapi.PropagatedPairs.
func SetSpan(ctx context.Context, span trace.Span) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(spanCtx, &propagatableSpan{Span: span})
}

// GetSpan retrieves the current span from the FrameContext
func GetSpan(ctx context.Context) (trace.Span, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	if val, ok := fc.Get(spanCtx); ok {
		// Handle wrapped span
		if ps, ok := val.(*propagatableSpan); ok {
			return ps.Span, true
		}
		// Handle raw span (for backwards compatibility)
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

// GetSpanContextKey returns the context key for storing span contexts
func GetSpanContextKey() *ctxapi.Key {
	return spanContextKey
}

// SpanContextPair creates a context pair for propagating span context to spawned processes
func SpanContextPair(sc trace.SpanContext) ctxapi.Pair {
	return ctxapi.Pair{Key: spanContextKey, Value: sc}
}
