// Package otel provides OpenTelemetry interceptor configuration.
package otel

// Config provides configuration for OpenTelemetry tracing interceptor.
type Config struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

// Options provides runtime options for OpenTelemetry span generation.
type Options struct {
	SpanName   string
	Attributes map[string]string
}
