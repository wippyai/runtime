package interceptor

import (
	"context"

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTelInterceptor implements OpenTelemetry tracing
type OTelInterceptor struct {
	tracer trace.Tracer
}

// NewOTelInterceptor creates a new OpenTelemetry interceptor
func NewOTelInterceptor(tracer trace.Tracer) *OTelInterceptor {
	return &OTelInterceptor{
		tracer: tracer,
	}
}

// Handle implements the interceptor interface
func (i *OTelInterceptor) Handle(ctx context.Context, next func(context.Context) (*runtime.Result, context.Context)) (*runtime.Result, context.Context) {
	if i.tracer == nil {
		// If tracer is not initialized, just return the result
		return next(ctx)
	}

	// Get PID from context first
	pid, ok := pubsub.GetPID(ctx)
	spanName := "function_execution"
	if ok {
		spanName = pid.ID.String()
	}

	// Check if there's an existing span in the context
	spanCtx := trace.SpanContextFromContext(ctx)
	var span trace.Span
	if spanCtx.IsValid() {
		// Create a sub-span if there's an existing span
		ctx, span = i.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))
	} else {
		// Create a new span if there's no existing span
		ctx, span = i.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
	}
	defer span.End()

	// Set PID as attribute if available
	if ok {
		span.SetAttributes(
			attribute.String("pid", pid.ID.String()),
		)
	}

	// Pass updated context to next
	result, newCtx := next(ctx)
	if result != nil && result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	}

	return result, newCtx
}
