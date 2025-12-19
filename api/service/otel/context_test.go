// Package otel provides OpenTelemetry service integration.
package otel

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/runtime"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWithTracer_GetTracer(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx = WithTracer(ctx, tracer)

	retrieved := GetTracer(ctx)
	assert.NotNil(t, retrieved)
	assert.Equal(t, tracer, retrieved)
}

func TestGetTracer_NoAppContext(t *testing.T) {
	ctx := context.Background()
	retrieved := GetTracer(ctx)
	assert.Nil(t, retrieved)
}

func TestGetTracer_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	retrieved := GetTracer(ctx)
	assert.Nil(t, retrieved)
}

func TestWithTracer_SetOnce(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	tracer1 := noop.NewTracerProvider().Tracer("test1")
	tracer2 := noop.NewTracerProvider().Tracer("test2")

	ctx = WithTracer(ctx, tracer1)
	ctx = WithTracer(ctx, tracer2)

	retrieved := GetTracer(ctx)
	assert.Equal(t, tracer1, retrieved)
}

func TestSetSpan_GetSpan(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, span := tracer.Start(ctx, "test-span")
	defer span.End()

	err := SetSpan(ctx, span)
	assert.NoError(t, err)

	retrieved, ok := GetSpan(ctx)
	assert.True(t, ok)
	assert.Equal(t, span, retrieved)
}

func TestGetSpan_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	retrieved, ok := GetSpan(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestGetSpan_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	retrieved, ok := GetSpan(ctx)
	assert.False(t, ok)
	assert.Nil(t, retrieved)
}

func TestSpanInheritance(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, parentFrame := ctxapi.OpenFrameContext(ctx)

	tracer := noop.NewTracerProvider().Tracer("test")
	ctx, parentSpan := tracer.Start(ctx, "parent-span")
	defer parentSpan.End()

	err := SetSpan(ctx, parentSpan)
	assert.NoError(t, err)

	parentFrame.Seal()

	childCtx, _ := ctxapi.OpenFrameContext(ctx)

	retrieved, ok := GetSpan(childCtx)
	assert.True(t, ok)
	assert.Equal(t, parentSpan, retrieved)
}

func TestWithService_GetService(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	ctx = WithService(ctx, &mockService{})

	service := GetService(ctx)
	assert.NotNil(t, service)
}

func TestGetService_NoAppContext(t *testing.T) {
	ctx := context.Background()
	service := GetService(ctx)
	assert.Nil(t, service)
}

func TestGetService_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	service := GetService(ctx)
	assert.Nil(t, service)
}

func TestWithService_NoAppContext(t *testing.T) {
	ctx := context.Background()
	result := WithService(ctx, &mockService{})
	assert.Equal(t, ctx, result)
}

func TestWithService_SetOnce(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	svc1 := &mockService{name: "first"}
	svc2 := &mockService{name: "second"}

	ctx = WithService(ctx, svc1)
	ctx = WithService(ctx, svc2)

	service := GetService(ctx)
	assert.NotNil(t, service)
	assert.Equal(t, "first", service.(*mockService).name)
}

func TestWithTracer_NoAppContext(t *testing.T) {
	ctx := context.Background()
	tracer := noop.NewTracerProvider().Tracer("test")
	result := WithTracer(ctx, tracer)
	assert.Equal(t, ctx, result)
}

func TestSetSpan_NoFrameContext(t *testing.T) {
	ctx := context.Background()
	tracer := noop.NewTracerProvider().Tracer("test")
	_, span := tracer.Start(ctx, "test-span")
	defer span.End()

	err := SetSpan(ctx, span)
	assert.NoError(t, err)
}

func TestGetSpanKey(t *testing.T) {
	key := GetSpanKey()
	assert.NotNil(t, key)
	assert.Equal(t, "otel.span", key.Name)
}

func TestGetRemoteSpanContext(t *testing.T) {
	t.Run("no frame context", func(t *testing.T) {
		ctx := context.Background()
		sc, ok := GetRemoteSpanContext(ctx)
		assert.False(t, ok)
		assert.False(t, sc.IsValid())
	})

	t.Run("not set", func(t *testing.T) {
		ctx := context.Background()
		appCtx := ctxapi.NewAppContext()
		ctx = ctxapi.WithAppContext(ctx, appCtx)
		ctx, _ = ctxapi.OpenFrameContext(ctx)

		sc, ok := GetRemoteSpanContext(ctx)
		assert.False(t, ok)
		assert.False(t, sc.IsValid())
	})
}

type mockService struct {
	name string
}

func (m *mockService) OnStart(_ context.Context, _ pid.PID, _ process.Process) error { return nil }
func (m *mockService) OnComplete(_ context.Context, _ pid.PID, _ *runtime.Result)    {}
func (m *mockService) HTTPMiddleware() func(http.Handler) http.Handler {
	return nil
}
func (m *mockService) Interceptor() function.Interceptor {
	return nil
}
func (m *mockService) QueuePublishInterceptor() queueapi.PublishInterceptor {
	return nil
}

func TestConfig(t *testing.T) {
	cfg := Config{
		Enabled:        true,
		Endpoint:       "http://localhost:4318",
		Protocol:       "grpc",
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
		Insecure:       true,
		SampleRate:     0.5,
		Propagators:    []string{"tracecontext", "baggage"},
		TracesEnabled:  true,
		MetricsEnabled: true,
		HTTP: HTTPConfig{
			Enabled:        true,
			ExtractHeaders: true,
			InjectHeaders:  true,
		},
		Process: ProcessConfig{
			Enabled:        true,
			TraceLifecycle: true,
		},
		Interceptor: InterceptorConfig{
			Enabled: true,
			Order:   1,
		},
		Queue: QueueConfig{
			Enabled: true,
		},
	}

	assert.True(t, cfg.Enabled)
	assert.Equal(t, "http://localhost:4318", cfg.Endpoint)
	assert.Equal(t, "grpc", cfg.Protocol)
	assert.Equal(t, "test-service", cfg.ServiceName)
	assert.Equal(t, 0.5, cfg.SampleRate)
	assert.True(t, cfg.HTTP.Enabled)
	assert.True(t, cfg.HTTP.ExtractHeaders)
	assert.True(t, cfg.Process.Enabled)
	assert.True(t, cfg.Interceptor.Enabled)
	assert.True(t, cfg.Queue.Enabled)
}
