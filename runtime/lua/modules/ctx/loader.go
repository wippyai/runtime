package ctx

import "github.com/ponyruntime/go-lua"

// Loader is the entry point for loading the plugin
func (m *Module[T]) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"get": m.get,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}
