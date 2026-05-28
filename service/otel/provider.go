// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"time"

	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials/insecure"
)

// Providers holds both TracerProvider and MeterProvider
type Providers struct {
	Tracer trace.TracerProvider
	Meter  metric.MeterProvider
}

// InitializeProvider creates and configures an OpenTelemetry TracerProvider
func InitializeProvider(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (trace.TracerProvider, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if !cfg.Enabled {
		logger.Debug("OTEL disabled, using noop provider")
		return noop.NewTracerProvider(), nil
	}

	exporter, err := createExporter(ctx, cfg, logger)
	if err != nil {
		return nil, newCreateExporterError(err)
	}

	res, err := createResource(cfg)
	if err != nil {
		return nil, newCreateResourceError(err)
	}

	sampler := createSampler(cfg)

	// Bounded BatchSpanProcessor: fixed memory ceiling on the in-process
	// queue, drop-on-overflow when the collector is unreachable. Matches
	// canonical OTel guidance for memory-bound deployments.
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxQueueSize(512),
			sdktrace.WithMaxExportBatchSize(128),
			sdktrace.WithBatchTimeout(2*time.Second),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithRawSpanLimits(sdktrace.SpanLimits{
			AttributeValueLengthLimit:   256,
			AttributeCountLimit:         16,
			EventCountLimit:             16,
			LinkCountLimit:              16,
			AttributePerEventCountLimit: 16,
			AttributePerLinkCountLimit:  16,
		}),
	)

	otel.SetTracerProvider(tp)

	// Configure OTEL SDK to use our centralized logger for internal errors
	// This ensures all OTEL internal errors (like connection refused)
	// go through our Zap logger instead of stderr
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		// Use the same logger instance that was passed to InitializeProvider
		logger.Debug("OTEL SDK internal error",
			zap.Error(err),
			zap.String("endpoint", cfg.Endpoint),
			zap.String("protocol", cfg.Protocol),
		)
	}))

	configurePropagators(cfg, logger)

	logger.Info("OTEL provider initialized",
		zap.String("service", cfg.ServiceName),
		zap.String("endpoint", cfg.Endpoint),
		zap.String("protocol", cfg.Protocol),
		zap.Float64("sample_rate", cfg.SampleRate))

	return tp, nil
}

// createExporter creates an OTLP trace exporter based on protocol
func createExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdktrace.SpanExporter, error) {
	if !cfg.TracesEnabled {
		logger.Debug("traces disabled, using noop exporter")
		return &noopExporter{}, nil
	}

	switch cfg.Protocol {
	case "grpc":
		return createGRPCExporter(ctx, cfg, logger)
	case "http/protobuf", "http":
		return createHTTPExporter(ctx, cfg, logger)
	default:
		return nil, newUnsupportedProtocolError(cfg.Protocol)
	}
}

// createGRPCExporter creates a gRPC OTLP exporter
func createGRPCExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()))
	}

	logger.Debug("creating gRPC exporter", zap.String("endpoint", cfg.Endpoint))

	return otlptracegrpc.New(ctx, opts...)
}

// createHTTPExporter creates an HTTP OTLP exporter
func createHTTPExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	logger.Debug("creating HTTP exporter", zap.String("endpoint", cfg.Endpoint))

	return otlptracehttp.New(ctx, opts...)
}

// createResource creates an OTEL resource with service information
func createResource(cfg otelapi.Config) (*resource.Resource, error) {
	attrs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
		),
	}

	if cfg.ServiceVersion != "" {
		attrs = append(attrs, resource.WithAttributes(
			semconv.ServiceVersion(cfg.ServiceVersion),
		))
	}

	return resource.New(
		context.Background(),
		attrs...,
	)
}

// createSampler creates a trace sampler based on sample rate
func createSampler(cfg otelapi.Config) sdktrace.Sampler {
	if cfg.SampleRate >= 1.0 {
		return sdktrace.AlwaysSample()
	}
	if cfg.SampleRate <= 0.0 {
		return sdktrace.NeverSample()
	}
	return sdktrace.TraceIDRatioBased(cfg.SampleRate)
}

// configurePropagators sets up trace context propagators
func configurePropagators(cfg otelapi.Config, logger *zap.Logger) {
	var propagators []propagation.TextMapPropagator

	for _, name := range cfg.Propagators {
		switch name {
		case "tracecontext":
			propagators = append(propagators, propagation.TraceContext{})
		case "baggage":
			propagators = append(propagators, propagation.Baggage{})
		default:
			logger.Warn("unknown propagator", zap.String("name", name))
		}
	}

	if len(propagators) > 0 {
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagators...))
		logger.Debug("propagators configured", zap.Strings("propagators", cfg.Propagators))
	}
}

// noopExporter is a no-op span exporter
type noopExporter struct{}

func (n *noopExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error {
	return nil
}

func (n *noopExporter) Shutdown(_ context.Context) error {
	return nil
}

// InitializeMeterProvider creates and configures an OpenTelemetry MeterProvider
func InitializeMeterProvider(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (metric.MeterProvider, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if !cfg.Enabled || !cfg.MetricsEnabled {
		logger.Debug("OTEL metrics disabled, using noop provider")
		return metricnoop.NewMeterProvider(), nil
	}

	exporter, err := createMetricExporter(ctx, cfg, logger)
	if err != nil {
		return nil, newCreateMetricExporterError(err)
	}

	res, err := createResource(cfg)
	if err != nil {
		return nil, newCreateResourceError(err)
	}

	// 30s push interval balances dashboard freshness (rate over 1m sees
	// 2 samples) with CPU/network overhead. The default of 60s is too
	// coarse for 1m rate queries; 30s is the sweet spot for low-CPU runs.
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter,
			sdkmetric.WithInterval(30*time.Second),
		)),
		sdkmetric.WithResource(res),
	)

	otel.SetMeterProvider(mp)

	logger.Info("OTEL meter provider initialized",
		zap.String("service", cfg.ServiceName),
		zap.String("endpoint", cfg.Endpoint))

	return mp, nil
}

func createMetricExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdkmetric.Exporter, error) {
	switch cfg.Protocol {
	case "grpc":
		return createGRPCMetricExporter(ctx, cfg, logger)
	case "http/protobuf", "http":
		return createHTTPMetricExporter(ctx, cfg, logger)
	default:
		return nil, newUnsupportedProtocolError(cfg.Protocol)
	}
}

func createGRPCMetricExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdkmetric.Exporter, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	logger.Debug("creating gRPC metric exporter", zap.String("endpoint", cfg.Endpoint))

	return otlpmetricgrpc.New(ctx, opts...)
}

func createHTTPMetricExporter(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (sdkmetric.Exporter, error) {
	opts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(cfg.Endpoint),
	}

	if cfg.Insecure {
		opts = append(opts, otlpmetrichttp.WithInsecure())
	}

	logger.Debug("creating HTTP metric exporter", zap.String("endpoint", cfg.Endpoint))

	return otlpmetrichttp.New(ctx, opts...)
}

// ShutdownMeterProvider gracefully shuts down the meter provider
func ShutdownMeterProvider(ctx context.Context, mp metric.MeterProvider, logger *zap.Logger) error {
	if logger == nil {
		logger = zap.NewNop()
	}
	if sdkMP, ok := mp.(*sdkmetric.MeterProvider); ok {
		logger.Debug("shutting down OTEL meter provider")
		if err := sdkMP.Shutdown(ctx); err != nil {
			return newShutdownMeterProviderError(err)
		}
		logger.Debug("OTEL meter provider shutdown complete")
	}
	return nil
}
