package logs

import (
	"context"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"go.uber.org/zap/zapcore"
)

// Core implements the api.Core interface and handles log interception and routing
type Core struct {
	downstream zapcore.Core
	bus        events.Bus
	config     atomic.Value // holds api.Config
}

// NewCore creates a new Core instance
func NewCore(downstream zapcore.Core, bus events.Bus) api.Core {
	c := &Core{
		downstream: downstream,
		bus:        bus,
	}

	// Set default configuration
	c.config.Store(api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.DebugLevel,
	})
	return c
}

// Configure implements api.Core
func (c *Core) Configure(cfg api.Config) {
	c.config.Store(cfg)
}

// GetConfig implements api.Core
func (c *Core) GetConfig() api.Config {
	return c.config.Load().(api.Config)
}

// Enabled implements zapcore.Core
func (c *Core) Enabled(level zapcore.Level) bool {
	cfg := c.config.Load().(api.Config)
	if level < cfg.MinLevel {
		return false
	}
	return cfg.PropagateDownstream || cfg.StreamToEvents
}

// With implements zapcore.Core
func (c *Core) With(fields []zapcore.Field) zapcore.Core {
	return &Core{
		downstream: c.downstream.With(fields),
		bus:        c.bus,
		config:     c.config,
	}
}

// Check implements zapcore.Core
func (c *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.Enabled(ent.Level) {
		return ce
	}

	// Always add our Core if enabled
	ce = ce.AddCore(ent, c)

	return ce
}

// Write implements zapcore.Core
func (c *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	cfg := c.config.Load().(api.Config)

	// Handle downstream propagation
	if cfg.PropagateDownstream {
		if err := c.downstream.Write(ent, fields); err != nil {
			return err
		}
	}

	// Handle event streaming
	if cfg.StreamToEvents {
		c.publishLogEvent(ent, fields)
	}

	return nil
}

// Sync implements zapcore.Core
func (c *Core) Sync() error {
	return c.downstream.Sync()
}

// publishLogEvent publishes the log entry to the event bus
func (c *Core) publishLogEvent(ent zapcore.Entry, fields []zapcore.Field) {
	c.bus.Send(context.Background(), events.Event{
		System: api.System,
		Kind:   api.EntryEvent,
		Path:   events.Path(ent.LoggerName),
		Data: struct {
			Entry  zapcore.Entry   `json:"entry"`
			Fields []zapcore.Field `json:"fields"`
		}{
			Entry:  ent,
			Fields: fields,
		},
	})
}
