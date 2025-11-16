package otel

import (
	"context"
	"fmt"

	otelapi "github.com/wippyai/runtime/api/service/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"go.uber.org/zap"
	"google.golang.org/grpc/credentials/insecure"
)

// InitializeProvider creates and configures an OpenTelemetry TracerProvider
func InitializeProvider(ctx context.Context, cfg otelapi.Config, logger *zap.Logger) (trace.TracerProvider, error) {
	if !cfg.Enabled {
		logger.Debug("OTEL disabled, using noop provider")
		return noop.NewTracerProvider(), nil
	}

	exporter, err := createExporter(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	res, err := createResource(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	sampler := createSampler(cfg)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(tp)

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
		return nil, fmt.Errorf("unsupported protocol: %s", cfg.Protocol)
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

// ShutdownProvider gracefully shuts down the tracer provider
func ShutdownProvider(ctx context.Context, tp trace.TracerProvider, logger *zap.Logger) error {
	if sdkTP, ok := tp.(*sdktrace.TracerProvider); ok {
		logger.Debug("shutting down OTEL provider")
		if err := sdkTP.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown tracer provider: %w", err)
		}
		logger.Debug("OTEL provider shutdown complete")
	}
	return nil
}
