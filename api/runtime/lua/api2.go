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
	Default     bool // Indicates the module is always loaded
	Build       func() (*lua.LTable, []YieldType)
	BuildValue  func() (lua.LValue, []YieldType) // For modules returning non-table values (e.g., userdata)

	once   sync.Once
	table  *lua.LTable
	value  lua.LValue
	yields []YieldType
}

// Load loads the module into LState. Returns yields.
func (m *ModuleDef) Load(l *lua.LState) []YieldType {
	m.once.Do(func() {
		if m.BuildValue != nil {
			m.value, m.yields = m.BuildValue()
		} else if m.Build != nil {
			m.table, m.yields = m.Build()
			m.value = m.table
		}
	})

	l.SetGlobal(m.Name, m.value)

	if pkg := l.GetGlobal("package"); pkg != lua.LNil {
		if pkgTbl, ok := pkg.(*lua.LTable); ok {
			if preload := pkgTbl.RawGetString("preload"); preload != lua.LNil {
				if preloadTbl, ok := preload.(*lua.LTable); ok {
					preloadTbl.RawSetString(m.Name, m.value)
				}
			}
		}
	}

	return m.yields
}

// Info returns module metadata.
// Deprecated: Use ModuleDef.Load() directly instead of Module interface.
func (m *ModuleDef) Info() ModuleInfo {
	return ModuleInfo{Name: m.Name, Description: m.Description, Class: m.Class}
}

// Register implements Module interface.
// Deprecated: Use ModuleDef.Load() directly instead of Module interface.
func (m *ModuleDef) Register(_ *lua.LState) *Registration {
	m.once.Do(func() {
		if m.BuildValue != nil {
			m.value, m.yields = m.BuildValue()
			if tbl, ok := m.value.(*lua.LTable); ok {
				m.table = tbl
			}
		} else if m.Build != nil {
			m.table, m.yields = m.Build()
			m.value = m.table
		}
	})
	return &Registration{Table: m.table, YieldTypes: m.yields}
}

// Loader implements Module interface.
// Deprecated: Use ModuleDef.Load() directly instead of Module interface.
func (m *ModuleDef) Loader(l *lua.LState) int {
	m.Register(l) // ensures once.Do is called
	if m.value != nil {
		l.Push(m.value)
	} else {
		l.Push(m.table)
	}
	return 1
}

// Module is the unified interface for Lua modules.
// Deprecated: Use ModuleDef directly with Load() method instead.
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

// ModuleV2 is an alias for Module.
// Deprecated: Use ModuleDef directly with Load() method instead.
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
// Deprecated: Use ModuleDef.Load() directly instead.
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
// Deprecated: Use ModuleDef.Load() directly instead.
func LoadModules(l *lua.LState, modules ...ModuleV2) []YieldType {
	var allYields []YieldType
	for _, m := range modules {
		yields := LoadModule(l, m)
		allYields = append(allYields, yields...)
	}
	return allYields
}
