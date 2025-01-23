package logs

import (
	"github.com/ponyruntime/pony/api/events"
	"go.uber.org/zap/zapcore"
)

const (
	// System identifies the logs system in the event context
	System           events.System = "logs"
	EntryEvent       events.Kind   = "logs.entry"
	SetConfigEvent   events.Kind   = "logs.config.set"
	GetConfigEvent   events.Kind   = "logs.config.get"
	ConfigStateEvent events.Kind   = "logs.config.state"
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
