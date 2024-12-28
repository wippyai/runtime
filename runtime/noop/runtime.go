package noop

import (
	"context"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// NoopRuntime implements a no-op runtime manager that satisfies the EntryListener interface
type NoopRuntime struct {
	logger *zap.Logger
}

// NewNoopRuntime creates a new instance of NoopRuntime
func NewNoopRuntime(logger *zap.Logger) *NoopRuntime {
	return &NoopRuntime{
		logger: logger,
	}
}

// Add implements EntryListener.Add - does nothing and returns nil
func (n *NoopRuntime) Add(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: add called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))
	return nil
}

// Update implements EntryListener.Update - does nothing and returns nil
func (n *NoopRuntime) Update(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: update called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))
	return nil
}

// Delete implements EntryListener.Delete - does nothing and returns nil
func (n *NoopRuntime) Delete(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: delete called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))
	return nil
}
