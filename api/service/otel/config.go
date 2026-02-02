package otel

// Config holds OpenTelemetry service configuration
type Config struct {
	Endpoint       string            `mapstructure:"endpoint"`
	Protocol       string            `mapstructure:"protocol"`
	ServiceName    string            `mapstructure:"service_name"`
	ServiceVersion string            `mapstructure:"service_version"`
	Propagators    []string          `mapstructure:"propagators"`
	Interceptor    InterceptorConfig `mapstructure:"interceptor"`
	SampleRate     float64           `mapstructure:"sample_rate"`
	HTTP           HTTPConfig        `mapstructure:"http"`
	Process        ProcessConfig     `mapstructure:"process"`
	Enabled        bool              `mapstructure:"enabled"`
	TracesEnabled  bool              `mapstructure:"traces_enabled"`
	MetricsEnabled bool              `mapstructure:"metrics_enabled"`
	Insecure       bool              `mapstructure:"insecure"`
	Queue          QueueConfig       `mapstructure:"queue"`
	Temporal       TemporalConfig    `mapstructure:"temporal"`
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
