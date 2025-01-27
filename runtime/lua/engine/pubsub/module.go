package pubsub

import (
	lua "github.com/yuin/gopher-lua"
)

// Module represents pubsub Lua module
type Module struct {
}

// NewModule creates a new pubsub module instance
func NewModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "pubsub"
}

// Loader registers the module functions
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Register functions
	L.SetField(mod, "subscribe", L.NewFunction(m.subscribeFunc))

	// Register module
	L.Push(mod)
	return 1
}

// subscribeFunc implements pubsub.subscribe(topic) -> yields subscription request
func (m *Module) subscribeFunc(L *lua.LState) int {
	topic := L.CheckString(1)
	if topic == "" {
		L.RaiseError("topic cannot be empty")
		return 0
	}

	// Create and yield subscription request
	L.Push(NewRequest(topic))
	return -1 // yield to scheduler
}
