package os_process

import (
	"github.com/ponyruntime/pony/code_executors/native"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func (m *Module) newProcess(l *lua.LState) int {
	log := getCtxLogger(l)

	// cmd should always be a non-empty string
	cmd := l.CheckString(1)
	ud := l.NewUserData()

	// table with the process options
	opts := l.CheckTable(2)

	log.Debug("creating new process", zap.String("cmd", cmd), zap.Any("opts", opts))

	// native process options
	nopts := make([]native.Options, 0, 3)

	// parse working directory
	wd := l.GetField(opts, "work_dir")
	if workdir, ok := wd.(lua.LString); ok {
		nopts = append(nopts, native.WithWorkingDir(workdir.String()))
	}

	// parse table with the environment variables
	// envs should be in the form of KEY=VALUE
	lenvs := l.GetField(opts, "env")
	if lenvst, ok := lenvs.(*lua.LTable); ok {
		goenvs := make(map[string]string)
		lenvst.ForEach(func(k lua.LValue, v lua.LValue) {
			goenvs[k.String()] = v.String()
		})

		nopts = append(nopts, native.WithEnv(goenvs))
	}

	nopts = append(nopts, native.WithCmd(cmd))

	executor := native.NewNativeExecutor(log.Named("native_exec"), nopts...)
	closer.FromContext(l.Context()).Add(func() error {
		// stop the executor
		executor.Stop()
		m.once = nil
		return nil
	})

	ud.Value = executor
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

func (m *Module) getStderr(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("[sync.Once operation]: getting process stderr stream")

	m.once.once.Do(func() {
		executor := getProcessExecutor(l)
		log.Debug("[sync.Once]: creating a new stderr stream")

		s, errs := stream.NewStream(l.Context(), executor.StderrReader(), stream.NewStreamConfig(65536))
		if errs != nil {
			l.RaiseError("failed to create stderr stream: %s", errs.Error())
			return
		}

		luaStream := &stream.LuaStream{Stream: s}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		// save the userdata
		m.once.value = ud
	})

	// push the saved userdata
	l.Push(m.once.value)

	return 1
}

func (m *Module) getStdout(l *lua.LState) int {
	log := getCtxLogger(l)
	log.Debug("[sync.Once operation]: getting process stdout stream")

	m.once.once.Do(func() {
		executor := getProcessExecutor(l)
		log.Debug("[sync.Once]: creating a new stdout stream")

		s, errs := stream.NewStream(l.Context(), executor.StdoutReader(), stream.NewStreamConfig(65536))
		if errs != nil {
			l.RaiseError("failed to create stdout stream: %s", errs.Error())
			return
		}

		luaStream := &stream.LuaStream{Stream: s}
		ud := l.NewUserData()
		ud.Value = luaStream
		l.SetMetatable(ud, l.GetTypeMetatable("Stream"))
		// save the userdata
		m.once.value = ud
	})

	// push the saved userdata
	l.Push(m.once.value)

	return 1
}

func (m *Module) signalProcess(l *lua.LState) int {
	sig := l.CheckInt(1)
	log := getCtxLogger(l)
	log.Debug("signaling process")
	executor := getProcessExecutor(l)
	err := executor.Signal(sig)
	if err != nil {
		log.Error("failed to signal the process", zap.Error(err))
		l.RaiseError("failed to signal the process: %s", err.Error())
		return 0
	}

	return 0
}

func (m *Module) writeStdin(l *lua.LState) int {
	// first arg is userdata
	// 2-nd arg is the data to write
	data := l.CheckString(2)
	log := getCtxLogger(l)
	log.Debug("sending data to the process stdin", zap.String("data", data))

	executor := getProcessExecutor(l)
	if executor == nil {
		log.Error("executor is nil")
		l.RaiseError("executor is nil")
		return 0
	}

	err := executor.WriteStdin([]byte(data))
	if err != nil {
		log.Error("failed to write to stdin", zap.Error(err))
		l.RaiseError("failed to write to stdin: %s", err.Error())
		return 0
	}

	return 0
}
