// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"context"
	"sync/atomic"

	api "github.com/wippyai/runtime/api/logs"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"go.uber.org/zap/zapcore"
)

type Core struct {
	downstream zapcore.Core
	bus        event.Bus
	config     *atomic.Value
	collector  atomic.Pointer[metrics.Collector]
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

func (c *Core) SetCollector(coll metrics.Collector) {
	if coll == nil {
		c.collector.Store(nil)
		return
	}
	for _, level := range []string{"debug", "info", "warn", "error", "dpanic", "panic", "fatal"} {
		coll.CounterAdd("runtime_log_emissions_total", 0, metrics.Labels{"level": level})
	}
	c.collector.Store(&coll)
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
	child := &Core{
		downstream: c.downstream.With(fields),
		bus:        c.bus,
		config:     c.config,
	}
	if coll := c.collector.Load(); coll != nil {
		child.collector.Store(coll)
	}
	return child
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
	c.recordEmission(ent.Level)

	return nil
}

func (c *Core) Sync() error {
	cfg := c.config.Load().(api.Config)

	if cfg.PropagateDownstream {
		return c.downstream.Sync()
	}

	return nil
}

func (c *Core) recordEmission(level zapcore.Level) {
	if coll := c.collector.Load(); coll != nil {
		(*coll).CounterInc("runtime_log_emissions_total", metrics.Labels{"level": level.String()})
	}
}

type logEntry struct {
	LoggerName string `json:"logger_name"`
	Message    string `json:"message"`
	Caller     string `json:"caller"`
	Stack      string `json:"stack"`
	Level      int    `json:"level"`
	Time       int64  `json:"time"`
}

type logField struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	String string `json:"string"`
	Int    int64  `json:"int"`
}

var fieldTypeNames = map[zapcore.FieldType]string{
	zapcore.StringType: "string",
	zapcore.Int64Type:  "int64",
	zapcore.Int32Type:  "int32",
	zapcore.Uint64Type: "uint64",
	zapcore.Uint32Type: "uint32",
	zapcore.Int16Type:  "int16",
	zapcore.Uint16Type: "uint16",
	zapcore.Int8Type:   "int8",
	zapcore.Uint8Type:  "uint8",
}

func fieldTypeToString(ft zapcore.FieldType) string {
	if name, ok := fieldTypeNames[ft]; ok {
		return name
	}
	return "unknown"
}

func (c *Core) publishLogEvent(ent zapcore.Entry, fields []zapcore.Field) {
	entry := logEntry{
		Level:      int(ent.Level),
		Time:       ent.Time.UnixNano(),
		LoggerName: ent.LoggerName,
		Message:    ent.Message,
		Caller:     ent.Caller.String(),
		Stack:      ent.Stack,
	}

	logFields := make([]logField, 0, len(fields))
	for _, f := range fields {
		field := logField{
			Key:  f.Key,
			Type: fieldTypeToString(f.Type),
		}

		switch f.Type {
		case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
			zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type:
			field.Int = f.Integer
		default:
			field.String = f.String
		}

		logFields = append(logFields, field)
	}

	// we only run this code in debug mode, so a second timeout is fine
	c.bus.Send(context.Background(), event.Event{
		System: api.System,
		Kind:   api.Entry,
		Path:   ent.LoggerName,
		Data: struct {
			Fields []logField `json:"fields"`
			Entry  logEntry   `json:"entry"`
		}{
			Entry:  entry,
			Fields: logFields,
		},
	})
}
