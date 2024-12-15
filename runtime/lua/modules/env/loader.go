package env

import (
	"github.com/ponyruntime/go-lua"
)

// Loader is the module loader function.
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"get": m.getKey,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}
