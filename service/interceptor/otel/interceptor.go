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
func (i *Interceptor) Handle(ctx context.Context, task runtime.Task, next func(context.Context, runtime.Task) (*runtime.Result, error)) (*runtime.Result, error) {
	tracer := otelapi.GetTracer(ctx)
	if tracer == nil {
		tracer = otel.GetTracerProvider().Tracer("pony-runtime")
	}
	if tracer == nil {
		return next(ctx, task)
	}

	// Use task ID for span name
	spanName := task.ID.String()
	if spanName == "" || spanName == ":" {
		spanName = "function_execution"
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
	result, err := next(ctx, task)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if result != nil && result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	}

	return result, err
}
