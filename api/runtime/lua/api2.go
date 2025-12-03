// Package lua2 provides the engine2 module API for Lua runtime integration.
package lua

import (
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	lua "github.com/yuin/gopher-lua"
)

// ModuleDef is the complete module definition.
// All fields are required.
type ModuleDef struct {
	Name        string
	Description string
	Class       []string
	Build       func() (*lua.LTable, []YieldType)

	once   sync.Once
	table  *lua.LTable
	yields []YieldType
}

// Load loads the module into LState. Returns yields.
func (m *ModuleDef) Load(l *lua.LState) []YieldType {
	m.once.Do(func() {
		m.table, m.yields = m.Build()
	})

	l.SetGlobal(m.Name, m.table)

	if pkg := l.GetGlobal("package"); pkg != lua.LNil {
		if pkgTbl, ok := pkg.(*lua.LTable); ok {
			if preload := pkgTbl.RawGetString("preload"); preload != lua.LNil {
				if preloadTbl, ok := preload.(*lua.LTable); ok {
					preloadTbl.RawSetString(m.Name, m.table)
				}
			}
		}
	}

	return m.yields
}

// Info returns module metadata. Implements Module interface for transition period.
func (m *ModuleDef) Info() ModuleInfo {
	return ModuleInfo{Name: m.Name, Description: m.Description, Class: m.Class}
}

// Register implements Module interface for transition period.
func (m *ModuleDef) Register(l *lua.LState) *Registration {
	m.once.Do(func() {
		m.table, m.yields = m.Build()
	})
	return &Registration{Table: m.table, YieldTypes: m.yields}
}

// Loader implements Module interface for transition period.
func (m *ModuleDef) Loader(l *lua.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// TODO: Remove Module interface, Registration struct, LoadModule, LoadModules
// once all modules are transitioned to ModuleDef

// Module is the unified interface for Lua modules.
// Modules provide metadata for filtering and registration for runtime setup.
type Module interface {
	// Info returns module metadata including name, description, and class tags.
	// Used for module filtering (DeniedClasses, AllowedClasses) and discovery.
	Info() ModuleInfo

	// Loader initializes the module in the given Lua state and returns the number of
	// values pushed onto the stack. Used for require() support.
	Loader(l *lua.LState) int

	// Register initializes the module and returns its configuration.
	// Called once per LState. The returned Registration contains
	// everything the process needs to set up the module.
	Register(l *lua.LState) *Registration
}

// ModuleV2 is an alias for Module for backward compatibility.
// Deprecated: Use Module instead.
type ModuleV2 = Module

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
// Sets the global, adds to package.preload (if available), and returns yield types.
// Zero allocation for preload when storing tables directly.
func LoadModule(l *lua.LState, m Module) []YieldType {
	info := m.Info()
	reg := m.Register(l)

	// Set global
	l.SetGlobal(info.Name, reg.Table)

	// Add to package.preload if package module is loaded
	// Store table directly (no closure) - our loRequire handles tables
	if pkg := l.GetGlobal("package"); pkg != lua.LNil {
		if pkgTbl, ok := pkg.(*lua.LTable); ok {
			if preload := pkgTbl.RawGetString("preload"); preload != lua.LNil {
				if preloadTbl, ok := preload.(*lua.LTable); ok {
					preloadTbl.RawSetString(info.Name, reg.Table)
				}
			}
		}
	}

	return reg.YieldTypes
}

// LoadModules loads multiple modules and returns all yield types.
func LoadModules(l *lua.LState, modules ...ModuleV2) []YieldType {
	var allYields []YieldType
	for _, m := range modules {
		yields := LoadModule(l, m)
		allYields = append(allYields, yields...)
	}
	return allYields
}
