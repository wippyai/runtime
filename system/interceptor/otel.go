package interceptor

import (
	"context"
	"fmt"

	apiinterceptor "github.com/ponyruntime/pony/api/interceptor"
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
func (i *OTelInterceptor) Handle(ctx context.Context, next func() *runtime.Result, _ ...apiinterceptor.Option) *runtime.Result {
	_, span := i.tracer.Start(ctx, "function_execution")
	defer span.End()

	fmt.Println("OTelInterceptor")

	// FIXME pass updated ctx to next

	result := next()
	if result != nil && result.Error != nil {
		span.RecordError(result.Error)
		span.SetStatus(codes.Error, result.Error.Error())
	}

	// span.SetAttributes(
	// 	attribute.String("function_id", task.ID.String()),
	// 	attribute.String("function_name", task.Name),
	// 	attribute.String("function_version", task.Version),
	// 	attribute.String("function_author", task.Author),
	// 	attribute.String("function_url", task.URL),
	// )

	span.End()

	fmt.Println("OTelInterceptor completed")

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
