package interceptor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const (
	serviceName       = "pony-runtime"
	collectorEndpoint = "localhost:4317"
	tempoEndpoint     = "localhost:3200"
)

// ensureOTelServicesRunning checks if OpenTelemetry services are running
func ensureOTelServicesRunning(t *testing.T) {
	// Check if Tempo is running
	resp, err := http.Get(fmt.Sprintf("http://%s/ready", tempoEndpoint))
	if err != nil {
		t.Skip("Tempo is not running, skipping integration test")
	}
	resp.Body.Close()

	// Check if OTLP collector is running
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(collectorEndpoint),
	)
	if err != nil {
		t.Skip("OpenTelemetry Collector is not running, skipping integration test")
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	require.NoError(t, err)

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(ctx)

	// Create and end a test span to verify tracing works
	_, span := otel.Tracer("test").Start(ctx, "otel_service_test")
	span.End()

	// Give some time for the span to be exported
	time.Sleep(1 * time.Second)

	// Verify the test span was exported
	traceResp, err := queryTraces(t, serviceName, time.Now().Add(-5*time.Second))
	if err != nil || len(traceResp.Data.Spans) == 0 {
		t.Skip("OpenTelemetry service is not properly exporting traces, skipping integration test")
	}

	// Verify we found our test span
	var foundTestSpan bool
	for _, span := range traceResp.Data.Spans {
		if span.OperationName == "otel_service_test" {
			foundTestSpan = true
			break
		}
	}
	if !foundTestSpan {
		t.Skip("Test span was not found in traces, skipping integration test")
	}
}

// TraceResponse represents the structure of Tempo's trace query response
type TraceResponse struct {
	Data struct {
		TraceID string `json:"traceID"`
		Spans   []struct {
			TraceID       string            `json:"traceID"`
			SpanID        string            `json:"spanID"`
			OperationName string            `json:"operationName"`
			StartTime     int64             `json:"startTime"`
			Duration      int64             `json:"duration"`
			Tags          map[string]string `json:"tags"`
			Status        struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"status"`
		} `json:"spans"`
	} `json:"data"`
}

// queryTraces queries Tempo API for traces
func queryTraces(t *testing.T, serviceName string, startTime time.Time) (*TraceResponse, error) {
	// Convert startTime to Unix timestamp in microseconds
	startTimeUnix := startTime.UnixNano() / 1000

	// Build the query URL
	url := fmt.Sprintf("http://%s/api/search?service=%s&start=%d", tempoEndpoint, serviceName, startTimeUnix)

	// Make the request
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to query traces: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse the response
	var traceResp TraceResponse
	if err := json.Unmarshal(body, &traceResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &traceResp, nil
}

// setupOTelExporter creates a real OTLP exporter for testing
func setupOTelExporter(t *testing.T) func() {
	ctx := context.Background()

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(collectorEndpoint),
	)
	require.NoError(t, err)

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	require.NoError(t, err)

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Return cleanup function
	return func() {
		require.NoError(t, tp.Shutdown(ctx))
	}
}

func TestOTelInterceptorWithRealExporter(t *testing.T) {
	// Skip if not running in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Ensure OpenTelemetry services are running
	ensureOTelServicesRunning(t)

	// Setup real OTLP exporter
	cleanup := setupOTelExporter(t)
	defer cleanup()

	// Create interceptor
	interceptor := NewOTelInterceptor()

	// Record start time for querying traces
	startTime := time.Now()

	// Create a test context with a parent span
	ctx, parentSpan := otel.Tracer("test").Start(context.Background(), "parent_span")
	defer parentSpan.End()

	// Add some attributes to make it easier to find in UI
	parentSpan.SetAttributes(semconv.ServiceNameKey.String("test-service"))

	// Execute test function
	result := interceptor.Handle(ctx, func() *runtime.Result {
		return &runtime.Result{
			Error: errors.New("test error"),
		}
	})

	// Verify result
	assert.NotNil(t, result)
	assert.Error(t, result.Error)

	// Give some time for the trace to be exported
	time.Sleep(2 * time.Second)

	// Query traces via API
	traceResp, err := queryTraces(t, serviceName, startTime)
	require.NoError(t, err)
	require.NotEmpty(t, traceResp.Data.Spans, "should find at least one span")

	// Verify spans
	var foundParentSpan, foundChildSpan bool
	for _, span := range traceResp.Data.Spans {
		switch span.OperationName {
		case "parent_span":
			foundParentSpan = true
			assert.Equal(t, "test-service", span.Tags["service.name"])
		case "function_execution":
			foundChildSpan = true
			assert.Equal(t, serviceName, span.Tags["service.name"])
			assert.Equal(t, "2", span.Status.Code) // Error status code
			assert.Contains(t, span.Status.Message, "test error")
		}
	}

	assert.True(t, foundParentSpan, "should find parent span")
	assert.True(t, foundChildSpan, "should find child span")
}

func TestOTelInterceptor(t *testing.T) {
	t.SkipNow()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	interceptor := NewOTelInterceptor()

	tests := []struct {
		name          string
		nextFunc      func() *runtime.Result
		expectedError error
		description   string
	}{
		{
			name: "successful execution",
			nextFunc: func() *runtime.Result {
				return &runtime.Result{}
			},
			expectedError: nil,
			description:   "should create a span for successful execution",
		},
		{
			name: "error execution",
			nextFunc: func() *runtime.Result {
				return &runtime.Result{Error: errors.New("test error")}
			},
			expectedError: errors.New("test error"),
			description:   "should record error in span",
		},
		{
			name: "nil result",
			nextFunc: func() *runtime.Result {
				return nil
			},
			expectedError: nil,
			description:   "should handle nil result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear spans from previous test
			exporter.Reset()

			// Create a new context with a parent span
			ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent_span")
			defer parentSpan.End()

			// Execute the interceptor
			result := interceptor.Handle(ctx, tt.nextFunc)

			// Verify the result
			if tt.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tt.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}

			// Get the spans
			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "should have one span")

			span := spans[0]
			assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")

			// Verify span status
			if tt.expectedError != nil {
				assert.Equal(t, codes.Error, span.Status.Code, "span status should be Error")
				assert.Equal(t, tt.expectedError.Error(), span.Status.Description, "span status description should match error")
			} else {
				assert.Equal(t, codes.Ok, span.Status.Code, "span status should be Ok")
			}

			// Verify parent-child relationship
			assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent.SpanID, "span should have correct parent")
		})
	}
}

func TestOTelInterceptorContextPropagation(t *testing.T) {
	t.SkipNow()
	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	interceptor := NewOTelInterceptor()

	// Create a context with a parent span
	ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent_span")
	defer parentSpan.End()

	// Execute the interceptor
	result := interceptor.Handle(ctx, func() *runtime.Result {
		return &runtime.Result{}
	})
	assert.NoError(t, result.Error)

	// Get the spans
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "should have one span")

	// Verify the span
	span := spans[0]
	assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")
	assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent.SpanID, "span should have correct parent")
}

func TestOTelInterceptorMultipleSpans(t *testing.T) {
	t.SkipNow()
	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	interceptor := NewOTelInterceptor()

	// Create a context with a parent span
	ctx, parentSpan := tp.Tracer("test").Start(context.Background(), "parent_span")
	defer parentSpan.End()

	// Execute multiple operations
	for i := 0; i < 3; i++ {
		result := interceptor.Handle(ctx, func() *runtime.Result {
			return &runtime.Result{}
		})
		assert.NoError(t, result.Error)
	}

	// Get the spans
	spans := exporter.GetSpans()
	require.Len(t, spans, 3, "should have three spans")

	// Verify each span
	for i, span := range spans {
		assert.Equal(t, "function_execution", span.Name, "span %d name should be 'function_execution'", i)
		assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent.SpanID, "span %d should have correct parent", i)
		assert.Equal(t, codes.Ok, span.Status.Code, "span %d status should be Ok", i)
	}
}

func TestOTelInterceptorBasic(t *testing.T) {
	// Create a new OTel interceptor
	interceptor := NewOTelInterceptor()

	// Create a test context
	ctx := context.Background()

	// Test successful execution
	t.Run("successful execution", func(t *testing.T) {
		successFunc := func() *runtime.Result {
			return &runtime.Result{
				Error: nil,
			}
		}

		result := interceptor.Handle(ctx, successFunc)
		assert.NotNil(t, result)
		assert.Nil(t, result.Error)
	})

	// Test error execution
	t.Run("error execution", func(t *testing.T) {
		errorFunc := func() *runtime.Result {
			return &runtime.Result{
				Error: assert.AnError,
			}
		}

		result := interceptor.Handle(ctx, errorFunc)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Error)
		assert.Equal(t, assert.AnError, result.Error)
	})
}
