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
	config     *atomic.Value
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

	if cfg.StreamToEvents {
		return true
	}

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

	if cfg.PropagateDownstream && ent.Level >= cfg.MinLevel {
		if err := c.downstream.Write(ent, fields); err != nil {
			return err
		}
	}

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

type LogEntry struct {
	Level      int    `json:"level"`
	Time       int64  `json:"time"`
	LoggerName string `json:"logger_name"`
	Message    string `json:"message"`
	Caller     string `json:"caller"`
	Stack      string `json:"stack"`
}

type LogField struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	String string `json:"string"`
	Int    int64  `json:"int"`
}

func fieldTypeToString(ft zapcore.FieldType) string {
	switch ft {
	case zapcore.StringType:
		return "string"
	case zapcore.Int64Type:
		return "int64"
	case zapcore.Int32Type:
		return "int32"
	case zapcore.Uint64Type:
		return "uint64"
	case zapcore.Uint32Type:
		return "uint32"
	case zapcore.Int16Type:
		return "int16"
	case zapcore.Uint16Type:
		return "uint16"
	case zapcore.Int8Type:
		return "int8"
	case zapcore.Uint8Type:
		return "uint8"
	default:
		return "unknown"
	}
}

func (c *Core) publishLogEvent(ent zapcore.Entry, fields []zapcore.Field) {
	logEntry := LogEntry{
		Level:      int(ent.Level),
		Time:       ent.Time.UnixNano(),
		LoggerName: ent.LoggerName,
		Message:    ent.Message,
		Caller:     ent.Caller.String(),
		Stack:      ent.Stack,
	}

	logFields := make([]LogField, 0, len(fields))
	for _, f := range fields {
		field := LogField{
			Key:  f.Key,
			Type: fieldTypeToString(f.Type),
		}

		switch f.Type {
		case zapcore.StringType:
			field.String = f.String
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type:
			field.Int = f.Integer
		case zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			field.Int = int64(f.Integer)
		default:
			field.String = f.String
		}

		logFields = append(logFields, field)
	}

	go c.bus.Send(context.Background(), event.Event{
		System: api.System,
		Kind:   api.Entry,
		Path:   ent.LoggerName,
		Data: struct {
			Entry  LogEntry   `json:"entry"`
			Fields []LogField `json:"fields"`
		}{
			Entry:  logEntry,
			Fields: logFields,
		},
	})
}
