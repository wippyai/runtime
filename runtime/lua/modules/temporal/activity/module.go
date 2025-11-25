package activity

import (
	"sync"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module provides Temporal activity context functions to Lua runtime
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// NewModule creates a new Temporal activity module instance
func NewModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "temporal.activity",
		Description: "Temporal activity context functions",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassNondeterministic},
	}
}

// Loader initializes and returns the module table for Lua
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates the module table with all functions
func (m *Module) initModuleTable(l *lua.LState) {
	mod := l.CreateTable(0, 6)

	// Activity context functions
	mod.RawSetString("info", l.NewFunction(m.info))
	mod.RawSetString("heartbeat", l.NewFunction(m.heartbeat))
	mod.RawSetString("get_heartbeat_details", l.NewFunction(m.getHeartbeatDetails))
	mod.RawSetString("has_heartbeat_details", l.NewFunction(m.hasHeartbeatDetails))
	mod.RawSetString("async_complete", l.NewFunction(m.asyncComplete))
	mod.RawSetString("is_canceled", l.NewFunction(m.isCanceled))

	mod.Immutable = true
	m.moduleTable = mod
}
