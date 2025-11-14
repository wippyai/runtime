package otel

import (
	"context"

	"github.com/wippyai/runtime/api/runtime"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Interceptor implements OpenTelemetry tracing
type Interceptor struct{}

// New creates a new OpenTelemetry interceptor
func New() *Interceptor {
	return &Interceptor{}
}

// Handle implements the interceptor interface
func (i *Interceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	tracer := otelapi.GetTracer(ctx)
	if tracer == nil {
		tracer = otel.GetTracerProvider().Tracer("pony-runtime")
	}
	if tracer == nil {
		return next(ctx)
	}

	// Get registry ID for span name
	registryID, ok := runtime.GetFrameID(ctx)
	spanName := "function_execution"
	if ok {
		spanName = registryID.String()
	}

	// Check for parent span from FrameContext
	parentSpan, hasParent := otelapi.GetSpan(ctx)
	var span trace.Span

	if hasParent {
		ctx, span = tracer.Start(trace.ContextWithSpan(ctx, parentSpan), spanName, trace.WithSpanKind(trace.SpanKindInternal))
	} else {
		ctx, span = tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
	}
	defer span.End()

	// Get PID and add as attribute
	if pid, ok := runtime.GetFramePID(ctx); ok {
		span.SetAttributes(
			attribute.String("pid", pid.String()),
		)
	}

	// Store span in FrameContext for child operations
	otelapi.SetSpan(ctx, span)

	// Pass context to next interceptor
	result, newCtx := next(ctx)
	if result != nil && result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	}

	return result, newCtx
}
