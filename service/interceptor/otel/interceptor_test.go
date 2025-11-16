package otel

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestContext(t *testing.T) (context.Context, *tracetest.SpanRecorder) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	tracer := tracerProvider.Tracer("pony-runtime")
	ctx = otelapi.WithTracer(ctx, tracer)

	return ctx, spanRecorder
}

func TestInterceptor_CreatesSpan(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	interceptor := New()

	called := false
	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, err := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
	assert.NoError(t, err)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "function_execution", spans[0].Name())
}

func TestInterceptor_UsesRegistryID(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	regID := registry.ID{NS: "test", Name: "function"}
	err := runtime.SetFrameID(ctx, regID)
	require.NoError(t, err)

	interceptor := New()

	task := runtime.Task{ID: regID}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	interceptor.Handle(ctx, task, next)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "test:function", spans[0].Name())
}

func TestInterceptor_AddsPIDAttribute(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	pid := relay.PID{Host: "testhost", UniqID: "test-pid-123"}
	err := runtime.SetFramePID(ctx, pid)
	require.NoError(t, err)

	interceptor := New()

	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	interceptor.Handle(ctx, task, next)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)

	attrs := spans[0].Attributes()
	found := false
	for _, attr := range attrs {
		if attr.Key == "pid" {
			found = true
			assert.Equal(t, "{testhost|test-pid-123}", attr.Value.AsString())
		}
	}
	assert.True(t, found, "PID attribute not found")
}

func TestInterceptor_RecordsError(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	interceptor := New()

	testErr := errors.New("test error")
	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Error: testErr}, nil
	}

	result, err := interceptor.Handle(ctx, task, next)

	assert.NotNil(t, result)
	assert.NoError(t, err)
	assert.Equal(t, testErr, result.Error)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Equal(t, "test error", spans[0].Status().Description)
}

func TestInterceptor_StoresSpanInFrameContext(t *testing.T) {
	ctx, _ := setupTestContext(t)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	interceptor := New()

	var capturedSpan interface{}
	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		span, ok := otelapi.GetSpan(ctx)
		if ok {
			capturedSpan = span
		}
		return &runtime.Result{}, nil
	}

	interceptor.Handle(ctx, task, next)

	assert.NotNil(t, capturedSpan)
}

func TestInterceptor_ParentSpanChaining(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)

	ctx, parentFrame := ctxapi.OpenFrameContext(ctx)

	interceptor := New()

	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		parentFrame.Seal()

		childCtx, _ := ctxapi.OpenFrameContext(ctx)

		childInterceptor := New()
		childTask := runtime.Task{}
		childNext := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
			return &runtime.Result{}, nil
		}

		return childInterceptor.Handle(childCtx, childTask, childNext)
	}

	interceptor.Handle(ctx, task, next)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 2)

	parentSpan := spans[1]
	childSpan := spans[0]

	assert.Equal(t, parentSpan.SpanContext().TraceID(), childSpan.SpanContext().TraceID())
	assert.Equal(t, parentSpan.SpanContext().SpanID(), childSpan.Parent().SpanID())
}

func TestInterceptor_NoTracerFallback(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	interceptor := New()

	called := false
	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, err := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
	assert.NoError(t, err)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
}

func TestInterceptor_NoFrameContext(t *testing.T) {
	ctx, spanRecorder := setupTestContext(t)

	interceptor := New()

	called := false
	task := runtime.Task{}
	next := func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, err := interceptor.Handle(ctx, task, next)

	assert.True(t, called)
	assert.NotNil(t, result)
	assert.NoError(t, err)

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
}
