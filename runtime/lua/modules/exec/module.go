package exec

import (
	"github.com/ponyruntime/pony/internal/codeexec/native"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
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

// Name returns the module's name.
func (m *Module) Name() string {
	return "exec"
} // todo: rename to exec

func (m *Module) Loader(l *lua.LState) int {
	// Pre-allocate module table with exact capacity
	t := l.CreateTable(0, 1) // Only one function: "new"

	// Directly register the function instead of using SetFuncs
	t.RawSetString("new", l.NewFunction(m.newProcess))

	// Register stream type
	stream.RegisterStream(l)

	// Register process type methods more efficiently
	mt := l.CreateTable(0, 1)          // __index field
	methodTable := l.CreateTable(0, 5) // 5 methods

	// Add methods directly to the method table
	methodTable.RawSetString("start", l.NewFunction(m.startProcess))
	methodTable.RawSetString("stderr_stream", l.NewFunction(m.getStderr))
	methodTable.RawSetString("stdout_stream", l.NewFunction(m.getStdout))
	methodTable.RawSetString("write_stdin", l.NewFunction(m.writeStdin))
	methodTable.RawSetString("signal", l.NewFunction(m.signalProcess))

	// Set __index field
	mt.RawSetString("__index", methodTable)

	// Register the metatable directly in registry
	l.G.Registry.RawSetString(metatableName, mt)

	l.Push(t)
	return 1
}
