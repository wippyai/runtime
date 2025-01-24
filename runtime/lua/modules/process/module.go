package process

import (
	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/code_executors/native"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const metatableName = "Process"

// TODO: process.new(name) // args not needed

type Module struct {
}

func NewModule() *Module {
	return &Module{}
}

func (m *Module) newProcess(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("creating new process")

	cmd := l.CheckString(1)
	ud := l.NewUserData()

	ud.Value = native.NewNativeExecutor(log.Named("native_exec"), native.WithCmd(cmd))
	l.SetMetatable(ud, l.GetTypeMetatable(metatableName))

	l.Push(ud)

	return 1
}

func (m *Module) startProcess(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("starting process")
	executor := getProcessExecutor(l)
	err := executor.Start()
	if err != nil {
		log.Error("failed to start the process", zap.Error(err))
		l.RaiseError("failed to start the process: %s", err.Error())
		return 0
	}

	return 0
}

func getProcessExecutor(l *lua.LState) *native.Executor {
	ud := l.CheckUserData(1)
	return ud.Value.(*native.Executor)
}

func getCtxLogger(l *lua.LState) *zap.Logger {
	ctx := l.Context()
	return ctx.Value(apic.LoggerCtx).(*zap.Logger)
}
