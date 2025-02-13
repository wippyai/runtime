package upstream

import (
	lua "github.com/yuin/gopher-lua"
)

// Module provides functionality to send values upstream from Lua
type Module struct {
	out chan<- any
}

// NewUpstreamModule creates a new upstream module instance
func NewUpstreamModule(out chan<- any) *Module {
	return &Module{
		out: out,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "upstream"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	// Spawn module table
	mod := l.NewTable()

	// Register functions
	l.SetField(mod, "send", l.NewFunction(m.send))

	// Register module
	l.Push(mod)
	return 1
}

// send implements upstream.send(value)
func (m *Module) send(l *lua.LState) int {
	select {
	case m.out <- l.CheckAny(1):
		l.Push(lua.LTrue)
	default:
		l.Push(lua.LFalse)
	}

	return 1
}
