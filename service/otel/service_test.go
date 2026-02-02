package otel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

func TestNewService(t *testing.T) {
	cfg := otelapi.Config{
		HTTP:        otelapi.HTTPConfig{Enabled: true},
		Process:     otelapi.ProcessConfig{Enabled: true},
		Interceptor: otelapi.InterceptorConfig{Enabled: true},
		Queue:       otelapi.QueueConfig{Enabled: true},
	}
	logger := zap.NewNop()
	provider := noop.NewTracerProvider()

	svc := NewService(cfg, logger, provider)

	require.NotNil(t, svc)
	assert.Equal(t, cfg, svc.cfg)
	assert.Equal(t, logger, svc.logger)
	assert.NotNil(t, svc.tracer)
}

func TestService_HTTPMiddleware_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		HTTP: otelapi.HTTPConfig{Enabled: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	middleware := svc.HTTPMiddleware()
	require.NotNil(t, middleware)

	// Verify it's a passthrough
	called := false
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	wrapped := middleware(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)
	assert.True(t, called)
}

func TestService_HTTPMiddleware_Enabled(t *testing.T) {
	cfg := otelapi.Config{
		HTTP: otelapi.HTTPConfig{
			Enabled:        true,
			ExtractHeaders: true,
			InjectHeaders:  true,
		},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	middleware := svc.HTTPMiddleware()
	require.NotNil(t, middleware)

	called := false
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	wrapped := middleware(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)
	assert.True(t, called)
}

func TestService_OnStart_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	err := svc.OnStart(context.Background(), pid.PID{}, nil)
	assert.NoError(t, err)
}

func TestService_OnStart_LifecycleDisabled(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	err := svc.OnStart(context.Background(), pid.PID{}, nil)
	assert.NoError(t, err)
}

func TestService_OnStart_NoSpanContext(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: true},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	err := svc.OnStart(context.Background(), pid.PID{UniqID: "test-pid"}, nil)
	assert.NoError(t, err)
}

func TestService_OnStart_WithSpanContext(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: true},
	}
	provider := noop.NewTracerProvider()
	svc := NewService(cfg, zap.NewNop(), provider)

	// Create a valid span context via frame context
	tracer := provider.Tracer("test")
	baseCtx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	ctx, fc := ctxapi.OpenFrameContext(baseCtx)
	defer ctxapi.ReleaseFrameContext(fc)

	spanCtx := span.SpanContext()
	_ = fc.Set(otelapi.GetSpanContextKey(), spanCtx)

	p := pid.PID{UniqID: "test-pid", Host: "test-host"}
	err := svc.OnStart(ctx, p, nil)
	assert.NoError(t, err)
}

func TestService_OnComplete_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	// Should not panic
	svc.OnComplete(context.Background(), pid.PID{}, nil)
}

func TestService_OnComplete_LifecycleDisabled(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	// Should not panic
	svc.OnComplete(context.Background(), pid.PID{}, nil)
}

func TestService_OnComplete_NoRemoteSpan(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: true},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	// Should not panic when no remote span context
	svc.OnComplete(context.Background(), pid.PID{UniqID: "test-pid"}, nil)
}

func TestService_OnComplete_WithResult(t *testing.T) {
	cfg := otelapi.Config{
		Process: otelapi.ProcessConfig{Enabled: true, TraceLifecycle: true},
	}
	provider := noop.NewTracerProvider()
	svc := NewService(cfg, zap.NewNop(), provider)

	tracer := provider.Tracer("test")
	baseCtx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	ctx, fc := ctxapi.OpenFrameContext(baseCtx)
	defer ctxapi.ReleaseFrameContext(fc)

	spanCtx := span.SpanContext()
	_ = fc.Set(otelapi.GetSpanContextKey(), spanCtx)

	p := pid.PID{UniqID: "test-pid"}

	// Test with nil result
	svc.OnComplete(ctx, p, nil)

	// Test with success result
	result := &runtime.Result{}
	svc.OnComplete(ctx, p, result)

	// Test with error result
	resultErr := &runtime.Result{Error: errors.New("test error")}
	svc.OnComplete(ctx, p, resultErr)
}

func TestService_Interceptor_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Interceptor: otelapi.InterceptorConfig{Enabled: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	interceptor := svc.Interceptor()
	assert.Nil(t, interceptor)
}

func TestService_Interceptor_Enabled(t *testing.T) {
	cfg := otelapi.Config{
		Interceptor: otelapi.InterceptorConfig{Enabled: true},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	interceptor := svc.Interceptor()
	require.NotNil(t, interceptor)
}

func TestService_QueuePublishInterceptor_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Queue: otelapi.QueueConfig{Enabled: false},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	interceptor := svc.QueuePublishInterceptor()
	assert.Nil(t, interceptor)
}

func TestService_QueuePublishInterceptor_Enabled(t *testing.T) {
	cfg := otelapi.Config{
		Queue: otelapi.QueueConfig{Enabled: true},
	}
	svc := NewService(cfg, zap.NewNop(), noop.NewTracerProvider())

	interceptor := svc.QueuePublishInterceptor()
	require.NotNil(t, interceptor)
}

func TestInterceptor_Handle_EmptySpanName(t *testing.T) {
	provider := noop.NewTracerProvider()
	inter := &interceptor{
		tracer: provider.Tracer("test"),
		logger: zap.NewNop(),
	}

	task := runtime.Task{
		ID: registry.ID{}, // Empty ID results in ":" or ""
	}

	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, err := inter.Handle(context.Background(), task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, called)
}

func TestInterceptor_Handle_WithParentSpan(t *testing.T) {
	provider := noop.NewTracerProvider()
	tracer := provider.Tracer("test")
	inter := &interceptor{
		tracer: tracer,
		logger: zap.NewNop(),
	}

	// Create parent span
	baseCtx, parentSpan := tracer.Start(context.Background(), "parent")
	defer parentSpan.End()

	ctx, fc := ctxapi.OpenFrameContext(baseCtx)
	defer ctxapi.ReleaseFrameContext(fc)

	err := otelapi.SetSpan(ctx, parentSpan)
	require.NoError(t, err)

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
	}

	called := false
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		called = true
		return &runtime.Result{}, nil
	}

	result, err := inter.Handle(ctx, task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, called)
}

func TestInterceptor_Handle_WithRemoteSpanContext(t *testing.T) {
	provider := noop.NewTracerProvider()
	tracer := provider.Tracer("test")
	inter := &interceptor{
		tracer: tracer,
		logger: zap.NewNop(),
	}

	// Create a span context
	baseCtx, span := tracer.Start(context.Background(), "remote")
	defer span.End()

	ctx, fc := ctxapi.OpenFrameContext(baseCtx)
	defer ctxapi.ReleaseFrameContext(fc)

	spanCtx := span.SpanContext()
	_ = fc.Set(otelapi.GetSpanContextKey(), spanCtx)

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
	}

	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	result, err := inter.Handle(ctx, task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestInterceptor_Handle_NextError(t *testing.T) {
	provider := noop.NewTracerProvider()
	inter := &interceptor{
		tracer: provider.Tracer("test"),
		logger: zap.NewNop(),
	}

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
	}

	expectedErr := errors.New("next error")
	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return nil, expectedErr
	}

	result, err := inter.Handle(context.Background(), task, next)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, result)
}

func TestInterceptor_Handle_ResultError(t *testing.T) {
	provider := noop.NewTracerProvider()
	inter := &interceptor{
		tracer: provider.Tracer("test"),
		logger: zap.NewNop(),
	}

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
	}

	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{Error: errors.New("result error")}, nil
	}

	result, err := inter.Handle(context.Background(), task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Error)
}

func TestInterceptor_Handle_WithFrameContext(t *testing.T) {
	provider := noop.NewTracerProvider()
	inter := &interceptor{
		tracer: provider.Tracer("test"),
		logger: zap.NewNop(),
	}

	// Create frame context with values
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	defer ctxapi.ReleaseFrameContext(fc)

	_ = runtime.SetFramePID(ctx, pid.PID{UniqID: "test-pid", Host: "test-host"})
	_ = runtime.SetFrameID(ctx, registry.NewID("test", "frame"))

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
	}

	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	result, err := inter.Handle(ctx, task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestInterceptor_Handle_WithBagOptions(t *testing.T) {
	provider := noop.NewTracerProvider()
	inter := &interceptor{
		tracer: provider.Tracer("test"),
		logger: zap.NewNop(),
	}

	task := runtime.Task{
		ID: registry.NewID("test", "function"),
		Options: runtime.Bag{
			"string_opt":  "value",
			"int_opt":     42,
			"int64_opt":   int64(100),
			"float64_opt": 3.14,
			"bool_opt":    true,
		},
	}

	next := func(_ context.Context, _ runtime.Task) (*runtime.Result, error) {
		return &runtime.Result{}, nil
	}

	result, err := inter.Handle(context.Background(), task, next)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}
