package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/service/otel"
	temporalinterceptor "github.com/wippyai/runtime/service/temporal/interceptor"
	"go.opentelemetry.io/otel/trace/noop"
	temporalotel "go.temporal.io/sdk/contrib/opentelemetry"
	sdkinterceptor "go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

func TestTemporal_NoAppContext(t *testing.T) {
	component := Temporal()
	assert.Equal(t, TemporalName, component.Name())

	ctx := context.Background()
	// Without app context, GetLogger returns noop logger, so component should succeed
	// but skip Temporal registration since no OTEL service is available
	newCtx, err := component.Load(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, newCtx)
}

func TestTemporal_NoOTELService(t *testing.T) {
	component := Temporal()

	ctx := context.Background()
	ctx = logapi.WithLogger(ctx, zap.NewNop())

	newCtx, err := component.Load(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, newCtx)
}

func TestTemporal_NoTracer(t *testing.T) {
	component := Temporal()

	ctx := context.Background()
	ctx = logapi.WithLogger(ctx, zap.NewNop())

	tp := noop.NewTracerProvider()
	cfg := otelapi.Config{Enabled: true}
	svc := otel.NewService(cfg, zap.NewNop(), tp)
	ctx = otelapi.WithService(ctx, svc)

	newCtx, err := component.Load(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, newCtx)
}

func TestTemporal_NoInterceptorRegistries(t *testing.T) {
	component := Temporal()

	ctx := context.Background()
	ctx = logapi.WithLogger(ctx, zap.NewNop())

	tp := noop.NewTracerProvider()
	cfg := otelapi.Config{Enabled: true}
	svc := otel.NewService(cfg, zap.NewNop(), tp)
	ctx = otelapi.WithService(ctx, svc)

	tracer := tp.Tracer("test")
	ctx = otelapi.WithTracer(ctx, tracer)

	newCtx, err := component.Load(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, newCtx)
}

func TestTemporal_RegistersInterceptor(t *testing.T) {
	component := Temporal()

	// Create context with AppContext for proper service registration
	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())

	// Add boot config with temporal tracing enabled
	bootCfg := boot.NewConfig(
		boot.WithSection("otel", map[string]any{
			"enabled":          true,
			"temporal.enabled": true,
		}),
	)
	ctx = boot.WithConfig(ctx, bootCfg)

	tp := noop.NewTracerProvider()
	cfg := otelapi.Config{Enabled: true, Temporal: otelapi.TemporalConfig{Enabled: true}}
	svc := otel.NewService(cfg, zap.NewNop(), tp)
	ctx = otelapi.WithService(ctx, svc)

	tracer := tp.Tracer("test")
	ctx = otelapi.WithTracer(ctx, tracer)

	clientReg := temporalinterceptor.NewClientRegistry()
	workerReg := temporalinterceptor.NewWorkerRegistry()
	ctx = temporalapi.WithClientInterceptorRegistry(ctx, clientReg)
	ctx = temporalapi.WithWorkerInterceptorRegistry(ctx, workerReg)

	// Verify context functions work
	retrievedClient := temporalapi.GetClientInterceptorRegistry(ctx)
	retrievedWorker := temporalapi.GetWorkerInterceptorRegistry(ctx)
	require.NotNil(t, retrievedClient, "client registry should be in context")
	require.NotNil(t, retrievedWorker, "worker registry should be in context")

	// Verify OTEL context
	retrievedSvc := otelapi.GetService(ctx)
	retrievedTracer := otelapi.GetTracer(ctx)
	require.NotNil(t, retrievedSvc, "service should be in context")
	require.NotNil(t, retrievedTracer, "tracer should be in context")

	newCtx, err := component.Load(ctx)
	require.NoError(t, err)
	assert.NotNil(t, newCtx)

	// Verify interceptors were registered
	clientInterceptors := clientReg.GetAll()
	workerInterceptors := workerReg.GetAll()

	assert.Len(t, clientInterceptors, 1, "should have one client interceptor")
	assert.Len(t, workerInterceptors, 1, "should have one worker interceptor")
}

func TestTemporal_DisabledInConfig(t *testing.T) {
	component := Temporal()

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())

	// Boot config with temporal tracing disabled
	bootCfg := boot.NewConfig(
		boot.WithSection("otel", map[string]any{
			"enabled":          true,
			"temporal.enabled": false,
		}),
	)
	ctx = boot.WithConfig(ctx, bootCfg)

	tp := noop.NewTracerProvider()
	cfg := otelapi.Config{Enabled: true}
	svc := otel.NewService(cfg, zap.NewNop(), tp)
	ctx = otelapi.WithService(ctx, svc)

	tracer := tp.Tracer("test")
	ctx = otelapi.WithTracer(ctx, tracer)

	clientReg := temporalinterceptor.NewClientRegistry()
	workerReg := temporalinterceptor.NewWorkerRegistry()
	ctx = temporalapi.WithClientInterceptorRegistry(ctx, clientReg)
	ctx = temporalapi.WithWorkerInterceptorRegistry(ctx, workerReg)

	newCtx, err := component.Load(ctx)
	require.NoError(t, err)
	assert.NotNil(t, newCtx)

	// Verify interceptors were NOT registered
	clientInterceptors := clientReg.GetAll()
	workerInterceptors := workerReg.GetAll()

	assert.Len(t, clientInterceptors, 0, "should have no client interceptor")
	assert.Len(t, workerInterceptors, 0, "should have no worker interceptor")
}

func TestTemporal_ComponentName(t *testing.T) {
	component := Temporal()
	assert.Equal(t, TemporalName, component.Name())
}

func TestTemporal_InterceptorTypeAssertion(t *testing.T) {
	// Verify that the Temporal OTEL interceptor implements both interfaces
	i, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})
	require.NoError(t, err)

	// The interceptor should implement both ClientInterceptor and WorkerInterceptor
	_, isClient := i.(sdkinterceptor.ClientInterceptor)
	_, isWorker := i.(sdkinterceptor.WorkerInterceptor)

	assert.True(t, isClient, "should implement ClientInterceptor")
	assert.True(t, isWorker, "should implement WorkerInterceptor")
}
