package exec

import (
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"

	// stream "github.com/ponyruntime/pony/runtime/lua/modules/stream" // No need to import directly if registered globally
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
	log *zap.Logger
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

// Name returns the module's name
func (m *Module) Name() string {
	return "exec"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *lua.LState) int {
	// Create the main module table
	mod := l.CreateTable(0, 1) // Only 'get' function initially

	// Register exec.get function (gets the Executor factory)
	mod.RawSetString("get", l.NewFunction(func(ls *lua.LState) int {
		return execGet(ls, m.log) // Returns Executor userdata
	}))

	// Register Executor type methods (factory)
	registerExecutor(l)

	// Register Process type methods (handle)
	registerProcess(l)

	stream.RegisterStream(l)

	// Push the module table
	l.Push(mod)
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
