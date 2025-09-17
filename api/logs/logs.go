// Package logs provides a structured logging system with context integration.
package logs

import (
	"github.com/ponyruntime/pony/api/event"
	"go.uber.org/zap/zapcore"
)

const (
	// System identifies the logs system in the event context
	System event.System = "logs"

	// Entry represents a log entry event in the system.
	Entry event.Kind = "logs.entry"

	// SetConfig represents a command to update log configuration.
	SetConfig event.Kind = "logs.config.set"
	// GetConfig represents a command to retrieve the current logging configuration.
	GetConfig event.Kind = "logs.config.get"
	// ConfigState represents a response event with the current state of log configuration.
	ConfigState event.Kind = "logs.config.state"
)

type (
	// Config represents the configuration state for log handling.
	Config struct {
		// PropagateDownstream controls whether logs are propagated to the underlying logger.
		// When true, logs will be sent to the configured downstream logging system. Turn off to keep IO free.
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
