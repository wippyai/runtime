package propagator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewReplayLogger(t *testing.T) {
	logger := zap.NewNop()
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)
	require.NotNil(t, rl)
	assert.Equal(t, logger, rl.logger)
}

func TestReplayLogger_LogsWhenNotReplaying(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)

	rl.Debug("debug msg")
	rl.Info("info msg")
	rl.Warn("warn msg")
	rl.Error("error msg")

	logs := recorded.All()
	require.Len(t, logs, 4)
	assert.Equal(t, "debug msg", logs[0].Message)
	assert.Equal(t, "info msg", logs[1].Message)
	assert.Equal(t, "warn msg", logs[2].Message)
	assert.Equal(t, "error msg", logs[3].Message)
}

func TestReplayLogger_SuppressedWhenReplaying(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	isReplaying := func() bool { return true }

	rl := NewReplayLogger(logger, isReplaying)

	rl.Debug("debug msg")
	rl.Info("info msg")
	rl.Warn("warn msg")
	rl.Error("error msg")

	logs := recorded.All()
	assert.Len(t, logs, 0, "logs should be suppressed during replay")
}

func TestReplayLogger_NilLogger(t *testing.T) {
	isReplaying := func() bool { return false }
	rl := NewReplayLogger(nil, isReplaying)

	// Should not panic
	rl.Debug("test")
	rl.Info("test")
	rl.Warn("test")
	rl.Error("test")
}

func TestReplayLogger_With(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)
	childRL := rl.With(zap.String("key", "value"))

	childRL.Info("msg with field")

	logs := recorded.All()
	require.Len(t, logs, 1)

	// Check that field is present
	fields := logs[0].Context
	require.Len(t, fields, 1)
	assert.Equal(t, "key", fields[0].Key)
	assert.Equal(t, "value", fields[0].String)
}

func TestReplayLogger_Named(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)
	namedRL := rl.Named("child")

	namedRL.Info("named msg")

	logs := recorded.All()
	require.Len(t, logs, 1)
	assert.Equal(t, "child", logs[0].LoggerName)
}

func TestReplayLogger_Check(t *testing.T) {
	core, _ := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)

	t.Run("not replaying returns entry", func(t *testing.T) {
		isReplaying := func() bool { return false }
		rl := NewReplayLogger(logger, isReplaying)

		entry := rl.Check(zapcore.InfoLevel, "test")
		assert.NotNil(t, entry)
	})

	t.Run("replaying returns nil", func(t *testing.T) {
		isReplaying := func() bool { return true }
		rl := NewReplayLogger(logger, isReplaying)

		entry := rl.Check(zapcore.InfoLevel, "test")
		assert.Nil(t, entry)
	})
}

func TestReplayLogger_Underlying(t *testing.T) {
	logger := zap.NewNop()
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)
	assert.Equal(t, logger, rl.Underlying())
}

func TestReplayLogger_Sync(t *testing.T) {
	logger := zap.NewNop()
	isReplaying := func() bool { return false }

	rl := NewReplayLogger(logger, isReplaying)
	err := rl.Sync()
	assert.NoError(t, err)
}
