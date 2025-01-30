package process

import (
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	lua "github.com/yuin/gopher-lua"
)

// Name returns the module's name.
func (m *Module) Name() string {
	return "process"
}

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
		"signal":        m.signalProcess,
		"write_stdin":   m.writeStdin,
		"stderr_stream": m.getStderr,
		"stdout_stream": m.getStdout,
	}))

	l.Push(t)

	return 1
}
