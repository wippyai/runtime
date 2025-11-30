// Package lua2 provides the engine2 module API for Lua runtime integration.
package lua2

import (
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module is the unified interface for engine2 modules.
// Modules provide metadata for filtering and registration for runtime setup.
type Module interface {
	// Info returns module metadata including name, description, and class tags.
	// Used for module filtering (DeniedClasses, AllowedClasses) and discovery.
	Info() luaapi.ModuleInfo

	// Register initializes the module and returns its configuration.
	// Called once per LState. The returned Registration contains
	// everything the process needs to set up the module.
	Register(l *lua.LState) *Registration
}

// Registration contains all module configuration returned by Register.
type Registration struct {
	// Table is the module table to set as global and preload
	Table *lua.LTable

	// YieldTypes are the yield types this module handles.
	// The process uses these to convert yields to dispatcher commands.
	YieldTypes []YieldType
}

// YieldType describes a yield and how to handle it.
type YieldType struct {
	// Sample is a zero-value instance used for type switching
	Sample any

	// CmdID is the dispatcher command ID for this yield
	CmdID dispatcher.CommandID
}

// YieldConverter is implemented by yield values to convert to dispatcher commands.
type YieldConverter interface {
	lua.LValue
	CmdID() dispatcher.CommandID
	ToCommand() dispatcher.Command
}

// Releasable is implemented by pooled yields for cleanup.
type Releasable interface {
	Release()
}

// LoadModule loads a module into the LState.
// Sets the global, adds to package.preload, and returns yield types.
func LoadModule(l *lua.LState, m Module) []YieldType {
	info := m.Info()
	reg := m.Register(l)

	// Set global
	l.SetGlobal(info.Name, reg.Table)

	// Add to package.preload for require() support
	l.PreloadModule(info.Name, func(l *lua.LState) int {
		l.Push(reg.Table)
		return 1
	})

	return reg.YieldTypes
}

// LoadModules loads multiple modules and returns all yield types.
func LoadModules(l *lua.LState, modules ...Module) []YieldType {
	var allYields []YieldType
	for _, m := range modules {
		yields := LoadModule(l, m)
		allYields = append(allYields, yields...)
	}
	return allYields
}
