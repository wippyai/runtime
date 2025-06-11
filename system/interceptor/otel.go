package interceptor

import (
	"context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// OTelInterceptor implements OpenTelemetry tracing
type OTelInterceptor struct {
	tracer trace.Tracer
}

// NewOTelInterceptor creates a new OpenTelemetry interceptor
func NewOTelInterceptor() *OTelInterceptor {
	return &OTelInterceptor{
		tracer: otel.Tracer("wippy"),
	}
}

// Handle implements the interceptor interface
func (i *OTelInterceptor) Handle(ctx context.Context, next func() *runtime.Result) *runtime.Result {
	_, span := i.tracer.Start(ctx, "function_execution")
	defer span.End()

	// Get PID from context
	pid, ok := pubsub.GetPID(ctx)
	if ok {
		span.SetAttributes(
			attribute.String("pid", pid.ID.String()),
		)
	}

	// Pass updated context to next
	result := next()
	if result != nil && result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	}

	return result
}

// Format implements the payload.Payload interface
func (i *OTelInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *OTelInterceptor) Data() any {
	return i
}
