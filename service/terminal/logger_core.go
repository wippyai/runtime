// logger/terminal_aware.go

package terminal

import (
	"go.uber.org/zap/zapcore"
	"log"
	"sync/atomic"
)

type LoggerCore struct {
	core       zapcore.Core
	isTerminal *atomic.Bool
}

// add comments, this core has to be used to redirect logs into servicve

func NewLoggerInterceptor(core zapcore.Core) *LoggerCore {
	return &LoggerCore{
		core:       core,
		isTerminal: &atomic.Bool{},
	}
}

func (t *LoggerCore) Enabled(level zapcore.Level) bool {
	if t.isTerminal.Load() {
		return false // Suppress all logs when terminal is active
	}
	return t.core.Enabled(level)
}

func (t *LoggerCore) With(fields []zapcore.Field) zapcore.Core {
	return &LoggerCore{
		core:       t.core.With(fields),
		isTerminal: t.isTerminal,
	}
}

func (t *LoggerCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if t.isTerminal.Load() {
		return ce // Skip logging when terminal is active
	}
	return t.core.Check(ent, ce)
}

func (t *LoggerCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	if t.isTerminal.Load() {
		log.Printf("Terminal is active, skipping log entry: %s", ent.Message)
		return nil // Skip writing when terminal is active
	}

	return t.core.Write(ent, fields)
}

func (t *LoggerCore) Sync() error {
	return t.core.Sync()
}

// SetTerminalActive sets whether the terminal is currently active
func (t *LoggerCore) SetTerminalActive(active bool) {
	t.isTerminal.Store(active)
}
