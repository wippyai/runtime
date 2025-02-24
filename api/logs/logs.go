package logs

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// loggerCtx is the context key used to store and retrieve the logger instance
var loggerCtx = &ctxapi.Key{Name: "logger"} //nolint:gochecknoglobals

const (
	// System identifies the logs system in the event context
	System events.System = "logs"

	// Entry represents a log entry event in the system.
	// This is used to identify individual log entries when they are published as events.
	Entry events.Kind = "logs.entry"

	// SetConfig represents a log configuration update event.
	// This event is triggered when the logging configuration is modified.
	SetConfig events.Kind = "logs.config.set"

	// GetConfig represents a log configuration retrieval event.
	// This event is triggered when the current logging configuration is requested.
	GetConfig events.Kind = "logs.config.get"

	// ConfigState represents the current state of log configuration.
	// This is used to track and report the current logging configuration state.
	ConfigState events.Kind = "logs.config.state"
)

type (
	// Config represents the configuration state for log handling.
	// It controls various aspects of log processing and distribution.
	Config struct {
		// PropagateDownstream controls whether logs are propagated to the underlying logger.
		// When true, logs will be sent to the configured downstream logging system.
		PropagateDownstream bool `json:"propagate_downstream"`

		// StreamToEvents controls whether logs are streamed to the event bus.
		// When true, logs will be published as events in the system.
		StreamToEvents bool `json:"stream_to_events"`

		// MinLevel is the minimum level of logs to process.
		// Logs below this level will be filtered out.
		MinLevel zapcore.Level `json:"min_level"`
	}

	// Core represents a configurable logging core that can be integrated into the system.
	// It extends the zapcore.Core interface with configuration management capabilities.
	Core interface {
		zapcore.Core

		// Configure updates the core's configuration with the provided settings.
		// This method should be thread-safe and handle concurrent configuration updates.
		Configure(cfg Config)

		// GetConfig returns the current configuration of the logging core.
		// This method should be thread-safe and provide consistent configuration state.
		GetConfig() Config
	}
)

// WithLogger creates a new context with the provided logger instance.
// This function is used to inject a logger into the context for later retrieval.
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerCtx, logger)
}

// GetLogger retrieves the logger instance from the provided context.
// If no logger is found in the context, it returns a no-op logger.
// This ensures that logging calls will not panic when no logger is configured.
func GetLogger(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerCtx).(*zap.Logger); ok {
		return l
	}

	return zap.NewNop()
}
