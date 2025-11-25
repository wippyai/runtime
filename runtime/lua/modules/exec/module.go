package exec

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/modules/stream"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

const (
	// Metatable name for the Executor factory userdata type
	executorMetatable = "exec.Executor"
	// Metatable name for the Process handle userdata type
	processMetatable = "exec.Process"
	// Metatable name for the Stream type from the stream module (Verify this name!)
	streamMetatable = "Stream"
)

// Module represents the exec Lua module
type Module struct {
	log         *zap.Logger
	once        sync.Once
	moduleTable *lua.LTable
}

// NewExecModule creates and returns a new instance of the exec Module
func NewExecModule(log *zap.Logger) *Module {
	if log == nil {
		log = zap.NewNop()
	}
	return &Module{
		log: log,
	}
}

func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "exec",
		Description: "External command execution",
		Class:       []string{luaapi.ClassIO},
	}
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		mod := l.CreateTable(0, 1)
		mod.RawSetString("get", l.NewFunction(func(ls *lua.LState) int {
			return execGet(ls, m.log)
		}))
		registerExecutor(l)
		registerProcess(l)
		stream.RegisterStream(l)
		mod.Immutable = true
		m.moduleTable = mod
	})
	l.Push(m.moduleTable)
	return 1
}

// registerExecutor registers methods for the Executor factory type
func registerExecutor(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"exec":    executorNewProcess, // todo: do not allow release while running processes, make error
		"release": executorRelease,    // Keep release for factory resource
	}
	value.RegisterMethods(l, executorMetatable, methods)
}

// registerProcess registers methods for the Process handle type
func registerProcess(l *lua.LState) {
	methods := map[string]lua.LGFunction{
		"start":         processStart,
		"stdout_stream": processStdoutStream,
		"stderr_stream": processStderrStream,
		"write_stdin":   processWriteStdin,
		"signal":        processSignal,
		"wait":          processWait,  // Only this one uses coroutine.Wrap
		"close":         processClose, // Renamed from release
	}
	value.RegisterMethods(l, processMetatable, methods)
}
