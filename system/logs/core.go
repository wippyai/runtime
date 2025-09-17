package logs

import (
	"context"
	"sync/atomic"

	api "github.com/ponyruntime/pony/api/logs"

	"github.com/ponyruntime/pony/api/event"
	"go.uber.org/zap/zapcore"
)

type Core struct {
	downstream zapcore.Core
	bus        event.Bus
	config     *atomic.Value // holds api.Config
}

func NewCore(downstream zapcore.Core, bus event.Bus) api.Core {
	c := &Core{
		downstream: downstream,
		bus:        bus,
		config:     &atomic.Value{},
	}

	c.config.Store(api.Config{
		PropagateDownstream: true,
		StreamToEvents:      false,
		MinLevel:            zapcore.DebugLevel,
	})
	return c
}

func (c *Core) Configure(cfg api.Config) {
	c.config.Store(cfg)
}

func (c *Core) GetConfig() api.Config {
	return c.config.Load().(api.Config)
}

func (c *Core) Enabled(level zapcore.Level) bool {
	cfg := c.config.Load().(api.Config)

	// Enable if event streaming is on (accepts all levels)
	if cfg.StreamToEvents {
		return true
	}

	// Enable if downstream is on AND level meets minimum threshold
	if cfg.PropagateDownstream && level >= cfg.MinLevel {
		return true
	}

	return false
}

func (c *Core) With(fields []zapcore.Field) zapcore.Core {
	return &Core{
		downstream: c.downstream.With(fields),
		bus:        c.bus,
		config:     c.config,
	}
}

func (c *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if !c.Enabled(ent.Level) {
		return ce
	}

	ce = ce.AddCore(ent, c)
	return ce
}

func (c *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	cfg := c.config.Load().(api.Config)

	// Send to downstream only if enabled AND level meets threshold
	if cfg.PropagateDownstream && ent.Level >= cfg.MinLevel {
		if err := c.downstream.Write(ent, fields); err != nil {
			return err
		}
	}

	// Always stream to events if enabled (no level filtering)
	if cfg.StreamToEvents {
		c.publishLogEvent(ent, fields)
	}

	return nil
}

func (c *Core) Sync() error {
	cfg := c.config.Load().(api.Config)

	if cfg.PropagateDownstream {
		return c.downstream.Sync()
	}

	return nil
}

func (c *Core) publishLogEvent(ent zapcore.Entry, fields []zapcore.Field) {
	c.bus.Send(context.Background(), event.Event{
		System: api.System,
		Kind:   api.Entry,
		Path:   ent.LoggerName,
		Data: struct {
			Entry  zapcore.Entry   `json:"entry"`
			Fields []zapcore.Field `json:"fields"`
		}{
			Entry:  ent,
			Fields: fields,
		},
	})
}
