package process

import (
	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/code_executors/native"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	metatableName = "Process"
)

type Module struct {
	once *onceStream
}

func NewModule() *Module {
	return &Module{
		once: newOnceStream(),
	}
}

func getProcessExecutor(l *lua.LState) *native.Executor {
	ud := l.CheckUserData(1)
	if ud == nil {
		return nil
	}

	switch tt := ud.Value.(type) {
	case *native.Executor:
		return tt
	default:
		return nil
	}
}

// no need to check for nil
func getCtxLogger(l *lua.LState) *zap.Logger {
	ctx := l.Context()
	if ctx == nil {
		return zap.NewNop()
	}

	switch tt := ctx.Value(apic.LoggerCtx).(type) {
	case *zap.Logger:
		return tt
	default:
		return zap.NewNop()
	}
}
