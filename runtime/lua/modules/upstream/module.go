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
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Register functions
	L.SetField(mod, "send", L.NewFunction(m.send))

	// Register module
	L.Push(mod)
	return 1
}

// send implements upstream.send(value)
func (m *Module) send(L *lua.LState) int {
	select {
	case m.out <- L.CheckAny(1):
		L.Push(lua.LTrue)
	default:
		L.Push(lua.LFalse)
	}

	return 1
}
