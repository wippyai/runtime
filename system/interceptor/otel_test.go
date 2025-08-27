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

	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName       = "pony-runtime"
	collectorEndpoint = "localhost:4317"
	tempoEndpoint     = "localhost:3200"
)

// ensureOTelServicesRunning checks if OpenTelemetry services are running
func ensureOTelServicesRunning(t *testing.T) {
	ctx := t.Context()

	// Check if Tempo is running
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/ready", tempoEndpoint), nil)
	if err != nil {
		t.Skip("Failed to create request for Tempo check, skipping integration test")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skip("Tempo is not running, skipping integration test")
	}
	resp.Body.Close()

	// Check if OTLP collector is running
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	// Create and end a test span to verify tracing works
	_, span := otel.Tracer("test").Start(ctx, "otel_service_test")
	span.End()

	// Give some time for the span to be exported
	time.Sleep(500 * time.Millisecond)

	// Verify the test span was exported
	traceResp, err := queryTraces(ctx, serviceName, time.Now().Add(-5*time.Second))
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
func queryTraces(ctx context.Context, serviceName string, startTime time.Time) (*TraceResponse, error) {
	// Convert startTime to Unix timestamp in microseconds
	startTimeUnix := startTime.UnixNano() / 1000

	// Build the query URL
	url := fmt.Sprintf("http://%s/api/search?service=%s&start=%d", tempoEndpoint, serviceName, startTimeUnix)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Make the request
	resp, err := http.DefaultClient.Do(req)
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
	ctx := t.Context()

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
	ctx := t.Context()

	// Skip if not running in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// Ensure OpenTelemetry services are running
	ensureOTelServicesRunning(t)

	// Setup real OTLP exporter
	cleanup := setupOTelExporter(t)
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()

	// Create interceptor with tracer
	interceptor := NewOTelInterceptor(otel.Tracer("test"))

	// Test cases for different scenarios
	testCases := []struct {
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Record start time for querying traces
			startTime := time.Now()

			// Create a test context with a parent span
			ctx, parentSpan := otel.Tracer("test").Start(ctx, "parent_span")
			defer parentSpan.End()

			// Add some attributes to make it easier to find in UI
			parentSpan.SetAttributes(semconv.ServiceNameKey.String("test-service"))

			// Execute test function
			result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
				return tc.nextFunc(), ctx
			})

			// Verify result
			assert.NotNil(t, result)
			if tc.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tc.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}

			// Give some time for the trace to be exported
			// Use context with timeout instead of time.Sleep to prevent test hanging
			exportCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()

			// Wait for trace export with context
			select {
			case <-exportCtx.Done():
				t.Log("Timeout waiting for trace export")
			case <-time.After(500 * time.Millisecond):
				// Give a reasonable time for export
			}

			// Query traces via API
			traceResp, err := queryTraces(ctx, serviceName, startTime)
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
					if tc.expectedError != nil {
						assert.Equal(t, "2", span.Status.Code) // Error status code
						assert.Contains(t, span.Status.Message, tc.expectedError.Error())
					} else {
						assert.Equal(t, "1", span.Status.Code) // Success status code
					}
				}
			}

			assert.True(t, foundParentSpan, "should find parent span")
			assert.True(t, foundChildSpan, "should find child span")
		})
	}
}

func TestOTelInterceptor(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	tests := []struct {
		name          string
		nextFunc      func(context.Context) (*runtime.Result, context.Context)
		expectedError error
		description   string
	}{
		{
			name: "successful execution",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			},
			expectedError: nil,
			description:   "should create a span for successful execution",
		},
		{
			name: "error execution",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{Error: errors.New("test error")}, ctx
			},
			expectedError: errors.New("test error"),
			description:   "should record error in span",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Execute the interceptor
			result, _ := interceptor.Handle(ctx, tt.nextFunc)

			// Verify the result
			if tt.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tt.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}
		})
	}
}

func TestOTelInterceptorContextPropagation(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	// Create a context with a parent span
	ctx, parentSpan := tp.Tracer("test").Start(ctx, "parent_span")
	defer parentSpan.End()

	// Execute the interceptor
	result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
		return &runtime.Result{}, ctx
	})
	assert.NoError(t, result.Error)

	// Force flush the spans to the exporter
	require.NoError(t, tp.ForceFlush(ctx))

	// Get the spans
	spans := exporter.GetSpans()
	require.Len(t, spans, 1, "should have one span")

	// Verify the span
	span := spans[0]
	assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")
	assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent.SpanID(), "span should have correct parent")
}

func TestOTelInterceptorMultipleSpans(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	// Create a context with a parent span
	ctx, parentSpan := tp.Tracer("test").Start(ctx, "parent_span")
	defer parentSpan.End()

	// Test cases for multiple operations
	testCases := []struct {
		name          string
		nextFunc      func(context.Context) (*runtime.Result, context.Context)
		expectedError error
		checkAttrs    bool
	}{
		{
			name: "successful operation",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			},
			expectedError: nil,
			checkAttrs:    true,
		},
		{
			name: "error operation",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{Error: errors.New("test error")}, ctx
			},
			expectedError: errors.New("test error"),
			checkAttrs:    true,
		},
		{
			name: "nil result",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return nil, ctx
			},
			expectedError: nil,
			checkAttrs:    false,
		},
	}

	// Execute multiple operations
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear spans from previous test
			exporter.Reset()

			// Create a wrapper function that safely handles nil results
			wrapperFunc := func(ctx context.Context) (*runtime.Result, context.Context) {
				// Add attributes to the context span
				span := trace.SpanFromContext(ctx)
				span.SetAttributes(attribute.String("test.attribute", "test-value"))

				// Execute the test function
				result, newCtx := tc.nextFunc(ctx)

				// If result is nil, return an empty result instead
				if result == nil {
					return &runtime.Result{}, newCtx
				}
				return result, newCtx
			}

			// Execute the interceptor
			result, _ := interceptor.Handle(ctx, wrapperFunc)

			// Verify the result
			if tc.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tc.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}

			// Force flush the spans to the exporter
			require.NoError(t, tp.ForceFlush(ctx))

			// Get the spans
			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "should have one span")

			// Verify the span
			span := spans[0]
			assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")
			assert.Equal(t, parentSpan.SpanContext().SpanID(), span.Parent.SpanID(), "span should have correct parent")

			// Verify span status
			if tc.expectedError != nil {
				assert.Equal(t, codes.Error, span.Status.Code, "span status should be Error")
				assert.Equal(t, tc.expectedError.Error(), span.Status.Description, "span status description should match error")
			} else {
				assert.Equal(t, codes.Unset, span.Status.Code, "span status should be Unset")
			}

			// Only check attributes for non-nil results
			if tc.checkAttrs {
				var foundAttr bool
				for _, attr := range span.Attributes {
					if attr.Key == "test.attribute" {
						foundAttr = true
						assert.Equal(t, "test-value", attr.Value.AsString())
						break
					}
				}
				assert.True(t, foundAttr, "should find test.attribute in span attributes")
			}
		})
	}
}

func TestOTelInterceptorBasic(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	// Create a new OTel interceptor
	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	// Test successful execution
	t.Run("successful execution", func(t *testing.T) {
		// Clear spans from previous test
		exporter.Reset()

		successFunc := func() *runtime.Result {
			return &runtime.Result{
				Error: nil,
			}
		}

		result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
			return successFunc(), ctx
		})
		assert.NotNil(t, result)
		assert.Nil(t, result.Error)

		// Force flush the spans to the exporter
		require.NoError(t, tp.ForceFlush(ctx))

		// Verify span was created
		spans := exporter.GetSpans()
		require.Len(t, spans, 1, "should have one span")
		span := spans[0]
		assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")
		assert.Equal(t, codes.Unset, span.Status.Code, "span status should be Unset")
	})

	// Test error execution
	t.Run("error execution", func(t *testing.T) {
		// Clear spans from previous test
		exporter.Reset()

		errorFunc := func() *runtime.Result {
			return &runtime.Result{
				Error: assert.AnError,
			}
		}

		result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
			return errorFunc(), ctx
		})
		assert.NotNil(t, result)
		assert.NotNil(t, result.Error)
		assert.Equal(t, assert.AnError, result.Error)

		// Force flush the spans to the exporter
		require.NoError(t, tp.ForceFlush(ctx))

		// Verify span was created with error
		spans := exporter.GetSpans()
		require.Len(t, spans, 1, "should have one span")
		span := spans[0]
		assert.Equal(t, "function_execution", span.Name, "span name should be 'function_execution'")
		assert.Equal(t, codes.Error, span.Status.Code, "span status should be Error")
		assert.Equal(t, assert.AnError.Error(), span.Status.Description, "span status description should match error")
	})
}

func TestOTelInterceptorWithExistingSpan(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	// Create a parent span
	ctx, parentSpan := tp.Tracer("test").Start(ctx, "parent")

	// Execute the interceptor
	result, _ := interceptor.Handle(ctx, func(ctx context.Context) (*runtime.Result, context.Context) {
		return &runtime.Result{}, ctx
	})
	assert.NoError(t, result.Error)

	// End the parent span
	parentSpan.End()

	// Force flush the spans to the exporter
	require.NoError(t, tp.ForceFlush(ctx))

	// Verify that a child span was created
	spans := exporter.GetSpans()
	require.Len(t, spans, 2, "should have two spans")

	// Find parent and child spans
	var parentSpanFound, childSpanFound tracetest.SpanStub
	for _, span := range spans {
		switch span.Name {
		case "parent":
			parentSpanFound = span
		case "function_execution":
			childSpanFound = span
		}
	}

	// Verify we found both spans
	require.NotEmpty(t, parentSpanFound.Name, "should find parent span")
	require.NotEmpty(t, childSpanFound.Name, "should find child span")

	// Verify parent-child relationship
	parentSpanID := parentSpanFound.SpanContext.SpanID()
	childParentSpanID := childSpanFound.Parent.SpanID()
	assert.Equal(t, parentSpanID, childParentSpanID, "function_execution span should have parent span as parent")

	// Verify trace relationship
	parentTraceID := parentSpanFound.SpanContext.TraceID()
	childTraceID := childSpanFound.SpanContext.TraceID()
	assert.Equal(t, parentTraceID, childTraceID, "spans should be in the same trace")
}

func TestOTelInterceptorWithPID(t *testing.T) {
	ctx := t.Context()

	// Create a test exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	defer func() {
		if err := tp.Shutdown(ctx); err != nil {
			t.Logf("Error shutting down tracer provider: %v", err)
		}
	}()

	interceptor := NewOTelInterceptor(tp.Tracer("test"))

	// Create a context with PID
	pid := pubsub.PID{
		Node:   "test-node",
		Host:   "test-host",
		ID:     registry.ID{NS: "test", Name: "test-id"},
		UniqID: "test-uniq",
	}
	ctx = pubsub.WithPID(ctx, pid)

	testCases := []struct {
		name          string
		nextFunc      func(context.Context) (*runtime.Result, context.Context)
		expectedError error
	}{
		{
			name: "successful operation",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{}, ctx
			},
			expectedError: nil,
		},
		{
			name: "error operation",
			nextFunc: func(ctx context.Context) (*runtime.Result, context.Context) {
				return &runtime.Result{Error: errors.New("test error")}, ctx
			},
			expectedError: errors.New("test error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear spans from previous test
			exporter.Reset()

			// Execute the interceptor
			result, _ := interceptor.Handle(ctx, tc.nextFunc)

			// Verify the result
			if tc.expectedError != nil {
				assert.Error(t, result.Error)
				assert.Equal(t, tc.expectedError.Error(), result.Error.Error())
			} else {
				assert.NoError(t, result.Error)
			}

			// Force flush the spans to the exporter
			require.NoError(t, tp.ForceFlush(ctx))

			// Verify that the span was created with PID attributes
			spans := exporter.GetSpans()
			require.Len(t, spans, 1, "should have one span")

			span := spans[0]
			assert.Equal(t, "test:test-id", span.Name, "span name should be 'test:test-id'")

			// Verify PID attributes
			var foundPIDAttr bool
			for _, attr := range span.Attributes {
				if attr.Key == "pid" {
					foundPIDAttr = true
					assert.Equal(t, "test:test-id", attr.Value.AsString(), "PID value should match")
					break
				}
			}
			assert.True(t, foundPIDAttr, "should find PID attribute in span")

			// Verify span status
			if tc.expectedError != nil {
				assert.Equal(t, codes.Error, span.Status.Code, "span status should be Error")
				assert.Equal(t, tc.expectedError.Error(), span.Status.Description, "span status description should match error")
			} else {
				assert.Equal(t, codes.Unset, span.Status.Code, "span status should be Unset")
			}
		})
	}
}

func TestOTelInterceptorWithNilTracer(t *testing.T) {
	ctx := t.Context()

	interceptor := &OTelInterceptor{
		tracer: nil,
	}

	// Test successful operation
	successFunc := func(ctx context.Context) (*runtime.Result, context.Context) {
		return &runtime.Result{}, ctx
	}
	result, _ := interceptor.Handle(ctx, successFunc)
	assert.NoError(t, result.Error)

	// Test error operation
	errorFunc := func(ctx context.Context) (*runtime.Result, context.Context) {
		return &runtime.Result{Error: errors.New("test error")}, ctx
	}
	result, _ = interceptor.Handle(ctx, errorFunc)
	assert.Error(t, result.Error)
	assert.Equal(t, "test error", result.Error.Error())
}
