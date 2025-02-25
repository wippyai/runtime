package exec

import (
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
)

// todo: refactor a little

// Name returns the module's name.
func (m *Module) Name() string {
	return "process"
} // todo: rename to exec

func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	api := map[string]lua.LGFunction{
		"new": m.newProcess,
	}

	l.SetFuncs(t, api)

	stream.RegisterStream(l)

	mt := l.NewTypeMetatable(metatableName)
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"start":         m.startProcess,
		"stderr_stream": m.getStderr,
		"stdout_stream": m.getStdout,
		"write_stdin":   m.writeStdin,
		"signal":        m.signalProcess,
	}))

	l.Push(t)

	return 1
}
