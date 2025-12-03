package client

import (
	"go.temporal.io/sdk/log"
	"go.uber.org/zap"
)

// zapAdapter adapts a zap.Logger to Temporal's log.Logger interface
type zapAdapter struct {
	zap *zap.Logger
}

// NewZapAdapter creates a Temporal logger from a zap logger
func NewZapAdapter(logger *zap.Logger) log.Logger {
	return &zapAdapter{zap: logger}
}

func (l *zapAdapter) Debug(msg string, keyvals ...interface{}) {
	l.zap.Debug(msg, l.zapFields(keyvals)...)
}

func (l *zapAdapter) Info(msg string, keyvals ...interface{}) {
	l.zap.Info(msg, l.zapFields(keyvals)...)
}

func (l *zapAdapter) Warn(msg string, keyvals ...interface{}) {
	l.zap.Debug(msg, l.zapFields(keyvals)...)
}

func (l *zapAdapter) Error(msg string, keyvals ...interface{}) {
	l.zap.Error(msg, l.zapFields(keyvals)...)
}

// zapFields converts key-value pairs to zap fields
func (l *zapAdapter) zapFields(keyvals []interface{}) []zap.Field {
	if len(keyvals) == 0 {
		return nil
	}

	fields := make([]zap.Field, 0, len(keyvals)/2)
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			key, ok := keyvals[i].(string)
			if !ok {
				continue
			}
			fields = append(fields, zap.Any(key, keyvals[i+1]))
		}
	}
	return fields
}
