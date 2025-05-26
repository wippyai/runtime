package otel

import (
	"context"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/system/functions/interceptor"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config represents OTEL interceptor configuration
type Config struct {
	Enabled          bool
	ServiceName      string
	CustomAttributes map[string]string
}

// Interceptor implements the interceptor interface for OpenTelemetry
type Interceptor struct {
	config Config
}

// New creates a new OTEL interceptor
func New(config Config) *Interceptor {
	return &Interceptor{
		config: config,
	}
}

// Before implements the interceptor interface
func (i *Interceptor) Before(ctx context.Context, execution *interceptor.Execution) error {
	if !i.config.Enabled {
		return nil
	}

	// Create a new span
	ctx, span := otel.Tracer(i.config.ServiceName).Start(ctx, execution.FunctionID)
	execution.Context = ctx

	// Add custom attributes
	for k, v := range i.config.CustomAttributes {
		span.SetAttributes(attribute.String(k, v))
	}

	// Add execution attributes
	span.SetAttributes(
		attribute.String("function.id", execution.FunctionID),
		attribute.String("start.time", execution.StartTime.Format(time.RFC3339)),
	)

	return nil
}

// After implements the interceptor interface
func (i *Interceptor) After(ctx context.Context, execution *interceptor.Execution, result interface{}, err error) error {
	if !i.config.Enabled {
		return nil
	}

	span := trace.SpanFromContext(ctx)
	if span == nil {
		return fmt.Errorf("no span found in context")
	}

	// Add end time
	execution.EndTime = time.Now()
	span.SetAttributes(attribute.String("end.time", execution.EndTime.Format(time.RFC3339)))

	// Add duration
	duration := execution.EndTime.Sub(execution.StartTime)
	span.SetAttributes(attribute.Int64("duration_ms", duration.Milliseconds()))

	// Add result or error
	if err != nil {
		span.SetAttributes(attribute.String("error", err.Error()))
		span.RecordError(err)
	} else if result != nil {
		span.SetAttributes(attribute.String("result", fmt.Sprintf("%v", result)))
	}

	span.End()
	return nil
}
