package mocklogger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ZapTestLogger(enab zapcore.LevelEnabler) (*zap.Logger, *ObservedLogs) {
	core, logs := New(enab)
	obsLog := zap.New(core, zap.Development())

	return obsLog, logs
}
