package interceptor

import (
	"github.com/ponyruntime/pony/api/runtime"
	"go.opentelemetry.io/otel/trace"
)

// Option defines a functional option for configuring interceptors
type Option func(*Options)

// Options holds the configuration for an interceptor
type Options struct {
	// Task is the runtime task being executed
	Task *runtime.Task
	// Metadata contains additional information about the execution
	Metadata map[string]interface{}
	// Span stores the OpenTelemetry span for tracing
	Span trace.Span
}

// WithTask sets the runtime task for the interceptor
func WithTask(task *runtime.Task) Option {
	return func(o *Options) {
		o.Task = task
	}
}

// WithMetadata adds metadata to the interceptor options
func WithMetadata(key string, value interface{}) Option {
	return func(o *Options) {
		if o.Metadata == nil {
			o.Metadata = make(map[string]interface{})
		}
		o.Metadata[key] = value
	}
}

// WithSpan sets the OpenTelemetry span for the interceptor
func WithSpan(span trace.Span) Option {
	return func(o *Options) {
		o.Span = span
	}
}

// NewOptions creates a new Options instance with the given options
func NewOptions(opts ...Option) *Options {
	options := &Options{
		Metadata: make(map[string]interface{}),
	}
	for _, opt := range opts {
		opt(options)
	}
	return options
}
