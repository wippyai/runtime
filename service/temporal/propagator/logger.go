package propagator

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ReplayLogger wraps a zap.Logger and suppresses logs during workflow replay.
// This ensures deterministic behavior and avoids duplicate log entries.
type ReplayLogger struct {
	logger      *zap.Logger
	isReplaying func() bool
}

// NewReplayLogger creates a replay-safe logger.
// The isReplaying function should return true during workflow replay.
func NewReplayLogger(logger *zap.Logger, isReplaying func() bool) *ReplayLogger {
	return &ReplayLogger{
		logger:      logger,
		isReplaying: isReplaying,
	}
}

// Debug logs a debug message if not replaying.
func (l *ReplayLogger) Debug(msg string, fields ...zap.Field) {
	if l.logger == nil || l.isReplaying() {
		return
	}
	l.logger.Debug(msg, fields...)
}

// Info logs an info message if not replaying.
func (l *ReplayLogger) Info(msg string, fields ...zap.Field) {
	if l.logger == nil || l.isReplaying() {
		return
	}
	l.logger.Info(msg, fields...)
}

// Warn logs a warning message if not replaying.
func (l *ReplayLogger) Warn(msg string, fields ...zap.Field) {
	if l.logger == nil || l.isReplaying() {
		return
	}
	l.logger.Warn(msg, fields...)
}

// Error logs an error message if not replaying.
func (l *ReplayLogger) Error(msg string, fields ...zap.Field) {
	if l.logger == nil || l.isReplaying() {
		return
	}
	l.logger.Error(msg, fields...)
}

// With creates a child logger with additional fields.
func (l *ReplayLogger) With(fields ...zap.Field) *ReplayLogger {
	return &ReplayLogger{
		logger:      l.logger.With(fields...),
		isReplaying: l.isReplaying,
	}
}

// Named creates a named child logger.
func (l *ReplayLogger) Named(name string) *ReplayLogger {
	return &ReplayLogger{
		logger:      l.logger.Named(name),
		isReplaying: l.isReplaying,
	}
}

// Check returns a CheckedEntry for conditional logging.
func (l *ReplayLogger) Check(lvl zapcore.Level, msg string) *zapcore.CheckedEntry {
	if l.isReplaying() {
		return nil
	}
	return l.logger.Check(lvl, msg)
}

// Sync flushes any buffered log entries.
func (l *ReplayLogger) Sync() error {
	return l.logger.Sync()
}

// Underlying returns the wrapped zap.Logger.
// Use this only when you need the raw logger (e.g., for third-party integrations).
func (l *ReplayLogger) Underlying() *zap.Logger {
	return l.logger
}
