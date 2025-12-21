package otel

// Config holds OpenTelemetry service configuration
type Config struct {
	// Enabled controls whether OTEL tracing is active
	Enabled bool `mapstructure:"enabled"`

	// Endpoint is the OTLP exporter endpoint (e.g., http://localhost:4318)
	Endpoint string `mapstructure:"endpoint"`

	// Protocol specifies the OTLP protocol: "grpc" or "http/protobuf"
	Protocol string `mapstructure:"protocol"`

	// ServiceName identifies the service in traces
	ServiceName string `mapstructure:"service_name"`

	// ServiceVersion is the service version for traces
	ServiceVersion string `mapstructure:"service_version"`

	// Insecure allows non-TLS connections when true
	Insecure bool `mapstructure:"insecure"`

	// SampleRate controls trace sampling (0.0 to 1.0)
	SampleRate float64 `mapstructure:"sample_rate"`

	// Propagators lists the propagators to use (e.g., ["tracecontext", "baggage"])
	Propagators []string `mapstructure:"propagators"`

	// TracesEnabled controls whether traces are collected
	TracesEnabled bool `mapstructure:"traces_enabled"`

	// MetricsEnabled controls whether metrics are collected
	MetricsEnabled bool `mapstructure:"metrics_enabled"`

	// HTTP configures HTTP middleware behavior
	HTTP HTTPConfig `mapstructure:"http"`

	// Process configures process lifecycle tracing
	Process ProcessConfig `mapstructure:"process"`

	// Interceptor configures function call interceptor
	Interceptor InterceptorConfig `mapstructure:"interceptor"`

	// Queue configures queue message tracing
	Queue QueueConfig `mapstructure:"queue"`

	// Temporal configures Temporal workflow tracing
	Temporal TemporalConfig `mapstructure:"temporal"`
}

// HTTPConfig configures HTTP middleware
type HTTPConfig struct {
	// Enabled controls whether HTTP middleware is registered
	Enabled bool `mapstructure:"enabled"`

	// ExtractHeaders enables extracting trace context from incoming requests
	ExtractHeaders bool `mapstructure:"extract_headers"`

	// InjectHeaders enables injecting trace context into responses
	InjectHeaders bool `mapstructure:"inject_headers"`
}

// ProcessConfig configures process lifecycle tracing
type ProcessConfig struct {
	// Enabled controls whether process hooks are registered
	Enabled bool `mapstructure:"enabled"`

	// TraceLifecycle enables tracing full process lifecycle
	TraceLifecycle bool `mapstructure:"trace_lifecycle"`
}

// InterceptorConfig configures function call interceptor
type InterceptorConfig struct {
	// Enabled controls whether the interceptor is registered
	Enabled bool `mapstructure:"enabled"`

	// Order specifies the interceptor execution order
	Order int `mapstructure:"order"`
}

// QueueConfig configures queue message tracing
type QueueConfig struct {
	// Enabled controls whether queue tracing is registered
	Enabled bool `mapstructure:"enabled"`
}

// TemporalConfig configures Temporal workflow tracing
type TemporalConfig struct {
	// Enabled controls whether Temporal tracing interceptor is registered
	Enabled bool `mapstructure:"enabled"`
}
