package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

var _ log.Logger = (*zapAdapter)(nil)

func newObservedLogger(level zapcore.Level) (*zap.Logger, *observer.ObservedLogs) {
	core, obs := observer.New(level)
	return zap.New(core), obs
}

func TestNewZapAdapter(t *testing.T) {
	logger := zap.NewNop()
	adapter := NewZapAdapter(logger)
	require.NotNil(t, adapter)
}

func TestNewZapAdapter_NilLogger(t *testing.T) {
	adapter := NewZapAdapter(nil)
	require.NotNil(t, adapter)
	// Should not panic on any log call
	adapter.Debug("test")
	adapter.Info("test")
	adapter.Warn("test")
	adapter.Error("test")
}

func TestZapAdapter_Debug(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.DebugLevel)
	adapter := NewZapAdapter(logger)

	adapter.Debug("debug message", "key", "value", "count", 42)

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, zapcore.DebugLevel, logs[0].Level)
	assert.Equal(t, "debug message", logs[0].Message)
	assert.Equal(t, "value", logs[0].ContextMap()["key"])
	assert.Equal(t, int64(42), logs[0].ContextMap()["count"])
}

func TestZapAdapter_Info(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.InfoLevel)
	adapter := NewZapAdapter(logger)

	adapter.Info("info message", "service", "temporal")

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, zapcore.InfoLevel, logs[0].Level)
	assert.Equal(t, "info message", logs[0].Message)
	assert.Equal(t, "temporal", logs[0].ContextMap()["service"])
}

func TestZapAdapter_Warn(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.WarnLevel)
	adapter := NewZapAdapter(logger)

	adapter.Warn("warn message")

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, zapcore.WarnLevel, logs[0].Level)
	assert.Equal(t, "warn message", logs[0].Message)
}

func TestZapAdapter_Error(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.ErrorLevel)
	adapter := NewZapAdapter(logger)

	adapter.Error("error message", "code", 500)

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, zapcore.ErrorLevel, logs[0].Level)
	assert.Equal(t, "error message", logs[0].Message)
	assert.Equal(t, int64(500), logs[0].ContextMap()["code"])
}

func TestZapAdapter_NoKeyvals(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.InfoLevel)
	adapter := NewZapAdapter(logger)

	adapter.Info("no fields")

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Empty(t, logs[0].ContextMap())
}

func TestZapAdapter_OddKeyvals(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.InfoLevel)
	adapter := NewZapAdapter(logger)

	// Odd number of keyvals - last key has no value, should be skipped
	adapter.Info("odd fields", "key1", "val1", "orphan")

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, "val1", logs[0].ContextMap()["key1"])
	_, hasOrphan := logs[0].ContextMap()["orphan"]
	assert.False(t, hasOrphan)
}

func TestZapAdapter_NonStringKey(t *testing.T) {
	logger, obs := newObservedLogger(zapcore.InfoLevel)
	adapter := NewZapAdapter(logger)

	// Non-string key should be skipped
	adapter.Info("bad key", 123, "val", "good", "ok")

	logs := obs.All()
	require.Len(t, logs, 1)
	assert.Equal(t, "ok", logs[0].ContextMap()["good"])
	_, has123 := logs[0].ContextMap()["123"]
	assert.False(t, has123)
}
