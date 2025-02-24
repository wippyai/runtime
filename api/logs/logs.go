package logs

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var loggerCtx = &ctxapi.Key{Name: "logger"} //nolint:gochecknoglobals

const (
	// System identifies the logs system in the event context
	System events.System = "logs"

	// EntryEvent represents a log entry event in the system
	EntryEvent events.Kind = "logs.entry"

	// SetConfigEvent represents a log configuration update event
	SetConfigEvent events.Kind = "logs.config.set"

	// GetConfigEvent represents a log configuration retrieval event
	GetConfigEvent events.Kind = "logs.config.get"

	// ConfigStateEvent represents the current state of log configuration
	ConfigStateEvent events.Kind = "logs.config.state"
)

type (
	// Config represents the configuration state for log handling
	Config struct {
		// PropagateDownstream controls whether logs are propagated to the underlying logger
		PropagateDownstream bool `json:"propagate_downstream"`

		// StreamToEvents controls whether logs are streamed to the event bus
		StreamToEvents bool `json:"stream_to_events"`

		// MinLevel is the minimum level of logs to process
		MinLevel zapcore.Level `json:"min_level"`
	}

	// Core represents a configurable logging core that can be integrated into the system
	Core interface {
		zapcore.Core

		// Configure updates the core's configuration
		Configure(cfg Config)

		// GetConfig returns the current configuration
		GetConfig() Config
	}
)

func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerCtx, logger)
}

func GetLogger(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerCtx).(*zap.Logger); ok {
		return l
	}

	return zap.NewNop()
}
