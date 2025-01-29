package process

import (
	"github.com/ponyruntime/pony/code_executors/native"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

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

func (m *Module) getStderr(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("getting process stderr")
	executor := getProcessExecutor(l)

	s, errs := stream.NewStream(l.Context(), executor.StderrReader(), stream.NewStreamConfig(65536))
	if errs != nil {
		l.RaiseError("failed to create stream: %s", errs.Error())
	}

	luaStream := &stream.LuaStream{Stream: s}
	ud := l.NewUserData()
	ud.Value = luaStream

	l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

	l.Push(ud)
	return 1
}

func (m *Module) getStdout(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("getting process stderr")
	executor := getProcessExecutor(l)

	s, errs := stream.NewStream(l.Context(), executor.StdoutReader(), stream.NewStreamConfig(65536))
	if errs != nil {
		l.RaiseError("failed to create stream: %s", errs.Error())
	}

	luaStream := &stream.LuaStream{Stream: s}
	ud := l.NewUserData()
	ud.Value = luaStream
	l.SetMetatable(ud, l.GetTypeMetatable("Stream"))

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

func (m *Module) closeProcess(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("closing process")
	executor := getProcessExecutor(l)
	executor.Stop()

	return 0
}
