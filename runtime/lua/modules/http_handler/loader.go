package httphandler

import "github.com/ponyruntime/go-lua"

func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()
	httpm := l.NewTypeMetatable(metatableName)

	// constructor
	l.SetFuncs(t, map[string]lua.LGFunction{
		"new": m.new,
	})

	l.SetField(httpm, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"method": m.method,
	}))

	l.Push(t)
	return 1
}
