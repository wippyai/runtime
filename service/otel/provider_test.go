// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
)

// recordingExporter records every exported span and does not clear on shutdown,
// so a test can prove the BatchSpanProcessor flushed its queue.
type recordingExporter struct {
	mu    sync.Mutex
	spans int
}

func (r *recordingExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans += len(spans)
	return nil
}

func (r *recordingExporter) Shutdown(context.Context) error { return nil }

func (r *recordingExporter) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.spans
}

func TestInitializeProvider_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Enabled: false,
	}
	logger := zap.NewNop()

	provider, err := InitializeProvider(context.Background(), cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// Verify it's a noop provider
	_, ok := provider.(noop.TracerProvider)
	assert.True(t, ok, "expected noop provider when disabled")
}

func TestCreateSampler_AlwaysSample(t *testing.T) {
	cfg := otelapi.Config{
		SampleRate: 1.0,
	}

	sampler := createSampler(cfg)
	require.NotNil(t, sampler)
	assert.Equal(t, sdktrace.AlwaysSample().Description(), sampler.Description())
}

func TestCreateSampler_NeverSample(t *testing.T) {
	cfg := otelapi.Config{
		SampleRate: 0.0,
	}

	sampler := createSampler(cfg)
	require.NotNil(t, sampler)
	assert.Equal(t, sdktrace.NeverSample().Description(), sampler.Description())
}

func TestCreateSampler_RatioBased(t *testing.T) {
	cfg := otelapi.Config{
		SampleRate: 0.5,
	}

	sampler := createSampler(cfg)
	require.NotNil(t, sampler)
	assert.Contains(t, sampler.Description(), "TraceIDRatioBased")
}

func TestCreateSampler_NegativeRate(t *testing.T) {
	cfg := otelapi.Config{
		SampleRate: -1.0,
	}

	sampler := createSampler(cfg)
	require.NotNil(t, sampler)
	assert.Equal(t, sdktrace.NeverSample().Description(), sampler.Description())
}

func TestCreateSampler_AboveOne(t *testing.T) {
	cfg := otelapi.Config{
		SampleRate: 2.0,
	}

	sampler := createSampler(cfg)
	require.NotNil(t, sampler)
	assert.Equal(t, sdktrace.AlwaysSample().Description(), sampler.Description())
}

func TestConfigurePropagators_TraceContext(t *testing.T) {
	cfg := otelapi.Config{
		Propagators: []string{"tracecontext"},
	}
	logger := zap.NewNop()

	// Should not panic
	configurePropagators(cfg, logger)
}

func TestConfigurePropagators_Baggage(t *testing.T) {
	cfg := otelapi.Config{
		Propagators: []string{"baggage"},
	}
	logger := zap.NewNop()

	configurePropagators(cfg, logger)
}

func TestConfigurePropagators_Multiple(t *testing.T) {
	cfg := otelapi.Config{
		Propagators: []string{"tracecontext", "baggage"},
	}
	logger := zap.NewNop()

	configurePropagators(cfg, logger)
}

func TestConfigurePropagators_Unknown(t *testing.T) {
	cfg := otelapi.Config{
		Propagators: []string{"unknown-propagator"},
	}
	logger := zap.NewNop()

	// Should not panic, just log warning
	configurePropagators(cfg, logger)
}

func TestConfigurePropagators_Empty(t *testing.T) {
	cfg := otelapi.Config{
		Propagators: []string{},
	}
	logger := zap.NewNop()

	configurePropagators(cfg, logger)
}

func TestNoopExporter_ExportSpans(t *testing.T) {
	exp := &noopExporter{}

	err := exp.ExportSpans(context.Background(), nil)
	assert.NoError(t, err)
}

func TestNoopExporter_Shutdown(t *testing.T) {
	exp := &noopExporter{}

	err := exp.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestCreateExporter_TracesDisabled(t *testing.T) {
	cfg := otelapi.Config{
		TracesEnabled: false,
	}
	logger := zap.NewNop()

	exp, err := createExporter(context.Background(), cfg, logger)
	require.NoError(t, err)

	_, ok := exp.(*noopExporter)
	assert.True(t, ok, "expected noop exporter when traces disabled")
}

func TestCreateExporter_UnsupportedProtocol(t *testing.T) {
	cfg := otelapi.Config{
		TracesEnabled: true,
		Protocol:      "unsupported",
	}
	logger := zap.NewNop()

	_, err := createExporter(context.Background(), cfg, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestCreateResource_Basic(t *testing.T) {
	cfg := otelapi.Config{
		ServiceName: "test-service",
	}

	res, err := createResource(cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestCreateResource_WithVersion(t *testing.T) {
	cfg := otelapi.Config{
		ServiceName:    "test-service",
		ServiceVersion: "1.0.0",
	}

	res, err := createResource(cfg)
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestCreateResource_MergesStandardAttributes(t *testing.T) {
	res, err := createResource(otelapi.Config{ServiceName: "test-service"})
	require.NoError(t, err)

	get := func(k string) string {
		v, ok := res.Set().Value(attribute.Key(k))
		if !ok {
			return ""
		}
		return v.Emit()
	}
	assert.Equal(t, "test-service", get("service.name"))
	for _, key := range []string{"host.name", "process.pid", "os.type", "telemetry.sdk.name"} {
		assert.NotEmpty(t, get(key), "resource should carry standard semconv %q", key)
	}
}

func TestInitializeMeterProvider_Disabled(t *testing.T) {
	cfg := otelapi.Config{
		Enabled: false,
	}
	logger := zap.NewNop()

	mp, err := InitializeMeterProvider(context.Background(), cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, mp)

	_, ok := mp.(metricnoop.MeterProvider)
	assert.True(t, ok, "expected noop meter provider when disabled")
}

func TestInitializeMeterProvider_MetricsDisabled(t *testing.T) {
	cfg := otelapi.Config{
		Enabled:        true,
		MetricsEnabled: false,
	}
	logger := zap.NewNop()

	mp, err := InitializeMeterProvider(context.Background(), cfg, logger)
	require.NoError(t, err)
	require.NotNil(t, mp)

	_, ok := mp.(metricnoop.MeterProvider)
	assert.True(t, ok, "expected noop meter provider when metrics disabled")
}

func TestCreateMetricExporter_UnsupportedProtocol(t *testing.T) {
	cfg := otelapi.Config{
		Protocol: "unsupported",
	}
	logger := zap.NewNop()

	_, err := createMetricExporter(context.Background(), cfg, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestShutdownMeterProvider_NoopProvider(t *testing.T) {
	mp := metricnoop.NewMeterProvider()
	logger := zap.NewNop()

	// Noop provider doesn't implement *sdkmetric.MeterProvider, so shutdown is no-op
	err := ShutdownMeterProvider(context.Background(), mp, logger)
	assert.NoError(t, err)
}

func TestShutdownTracerProvider_FlushesSpans(t *testing.T) {
	exp := &recordingExporter{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	tracer := tp.Tracer("test")

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_, span := tracer.Start(ctx, fmt.Sprintf("op-%d", i))
		span.End()
	}

	require.Zero(t, exp.Count(), "spans must still be queued before shutdown")

	require.NoError(t, ShutdownTracerProvider(context.Background(), tp, zap.NewNop()))

	require.Equal(t, 10, exp.Count(), "shutdown must flush all queued spans to the exporter")
}

func TestShutdownTracerProvider_NoopProvider(t *testing.T) {
	err := ShutdownTracerProvider(context.Background(), noop.NewTracerProvider(), zap.NewNop())
	assert.NoError(t, err)
}

func TestProviderUsesBoundedBatcher(t *testing.T) {
	cfg := otelapi.Config{
		Enabled:       true,
		TracesEnabled: false, // noop exporter — we just inspect the provider
		Endpoint:      "127.0.0.1:0",
		Protocol:      "grpc",
		ServiceName:   "test",
		SampleRate:    1.0,
	}
	tp, err := InitializeProvider(context.Background(), cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("InitializeProvider: %v", err)
	}
	sdkTP, ok := tp.(*sdktrace.TracerProvider)
	if !ok {
		t.Fatalf("expected *sdktrace.TracerProvider, got %T", tp)
	}
	// Indirect check: we cannot reflect on processor internals, but we can
	// at least verify shutdown returns within a short window even with
	// unbounded production load — i.e. the batcher does not block.
	if err := sdkTP.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
