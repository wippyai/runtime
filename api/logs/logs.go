// SPDX-License-Identifier: MPL-2.0

// Package logs provides a structured logging system with context integration.
package logs

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap/zapcore"
)

// System identifies the logs system in the event bus.
const System event.System = "logs"

// Event kinds for log operations.
const (
	Entry       event.Kind = "logs.entry"
	SetConfig   event.Kind = "logs.config.set"
	GetConfig   event.Kind = "logs.config.get"
	ConfigState event.Kind = "logs.config.state"
)

type (
	// Config represents the configuration state for log handling.
	Config struct {
		PropagateDownstream bool          `json:"propagate_downstream"`
		StreamToEvents      bool          `json:"stream_to_events"`
		MinLevel            zapcore.Level `json:"min_level"`
	}

	// Core extends zapcore.Core with configuration management.
	Core interface {
		zapcore.Core
		Configure(cfg Config)
		GetConfig() Config
		SetCollector(c metrics.Collector)
	}

	// Manager represents a logging manager that can be started and stopped.
	Manager interface {
		Start(ctx context.Context) error
		Stop() error
		GetConfig() Config
		SetCollector(c metrics.Collector)
	}
)
