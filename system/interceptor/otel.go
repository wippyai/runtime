package interceptor

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"go.opentelemetry.io/otel"
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
		tracer: otel.Tracer("pony"),
	}
}

// Handle implements the interceptor interface
func (i *OTelInterceptor) Handle(ctx context.Context, task *runtime.Task, next func() error, opts ...Option) error {
	_, span := i.tracer.Start(ctx, "function_execution")
	defer span.End()

	fmt.Println("OTelInterceptor")

	// FIXME pass updated ctx to next

	err := next()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	// span.SetAttributes(
	// 	attribute.String("function_id", task.ID.String()),
	// 	attribute.String("function_name", task.Name),
	// 	attribute.String("function_version", task.Version),
	// 	attribute.String("function_author", task.Author),
	// 	attribute.String("function_url", task.URL),
	// )

	span.End()

	return err
}

// Format implements the payload.Payload interface
func (i *OTelInterceptor) Format() payload.Format {
	return payload.Golang
}

// Data implements the payload.Payload interface
func (i *OTelInterceptor) Data() any {
	return i
}
