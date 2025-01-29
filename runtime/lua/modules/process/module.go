package process

import (
	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/code_executors/native"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	metatableName     = "proc"
	processStreamName = "process_stream"
)

// TODO: process.new(name) // args not needed

type Module struct{}

func NewModule() *Module {
	return &Module{}
}

func getProcessExecutor(l *lua.LState) *native.Executor {
	ud := l.CheckUserData(1)
	return ud.Value.(*native.Executor)
}

func getCtxLogger(l *lua.LState) *zap.Logger {
	ctx := l.Context()
	return ctx.Value(apic.LoggerCtx).(*zap.Logger)
}
