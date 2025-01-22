// logger/terminal_aware.go

package logger

import (
	"go.uber.org/zap/zapcore"
	"log"
	"sync/atomic"
)

type Core struct {
	core       zapcore.Core
	isTerminal *atomic.Bool
}

// add comments, this core has to be used to redirect logs into servicve

func NewTerminalLoggerCore(core zapcore.Core) *Core {
	return &Core{
		core:       core,
		isTerminal: &atomic.Bool{},
	}
}

func (t *Core) Enabled(level zapcore.Level) bool {
	if t.isTerminal.Load() {
		return false // Suppress all logs when terminal is active
	}
	return t.core.Enabled(level)
}

func (t *Core) With(fields []zapcore.Field) zapcore.Core {
	return &Core{
		core:       t.core.With(fields),
		isTerminal: t.isTerminal,
	}
}

func (t *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if t.isTerminal.Load() {
		return ce // Skip logging when terminal is active
	}
	return t.core.Check(ent, ce)
}

func (t *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	if t.isTerminal.Load() {
		log.Printf("Terminal is active, skipping log entry: %s", ent.Message)
		return nil // Skip writing when terminal is active
	}

	return t.core.Write(ent, fields)
}

func (t *Core) Sync() error {
	return t.core.Sync()
}

// SetTerminalActive sets whether the terminal is currently active
func (t *Core) SetTerminalActive(active bool) {
	t.isTerminal.Store(active)
}
