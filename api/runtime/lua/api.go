// Package lua provides Lua runtime integration.
package lua

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/types"
)

const (
	System          event.System = "lua"
	InvalidateNodes event.Kind   = "lua.reset_code"
)

// Module class constants for consistent categorization
const (
	ClassDeterministic    = "deterministic"    // Same input = same output
	ClassNondeterministic = "nondeterministic" // Output varies (time, random)
	ClassIO               = "io"               // External I/O operations
	ClassNetwork          = "network"          // Network operations
	ClassEncoding         = "encoding"         // Data serialization
	ClassTime             = "time"             // Time-related
	ClassProcess          = "process"          // Process management
	ClassSecurity         = "security"         // Security operations
	ClassStorage          = "storage"          // Data storage
	ClassWorkflow         = "workflow"         // Workflow-safe replacements
)

// ModuleInfo contains metadata about a Lua module
type ModuleInfo struct {
	Name        string   // Module identifier (used for require)
	Description string   // Human-readable description
	Class       []string // Tags using Class* constants
}

type (
	// Factory creates new instances of the Lua virtual machine with compiled code.
	Factory interface {
		Compile() error
		CreateVM() (VM, error)
	}

	// VM represents a Lua virtual machine instance.
	VM interface {
		Execute(ctx context.Context, name string, args ...lua.LValue) (lua.LValue, error)
		Close()
	}
)

// Module is the interface for Lua modules.
type Module interface {
	Info() ModuleInfo
	Loader(l *lua.LState) int
	Register(l *lua.LState) *Registration
}

// Registration contains module configuration returned by Register.
type Registration struct {
	Table      *lua.LTable
	YieldTypes []YieldType
}

// YieldType describes a yield and how to handle it.
type YieldType struct {
	Sample any
	CmdID  dispatcher.CommandID
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

// HandledYield is implemented by yields that know how to convert
// handler results back to Lua values.
type HandledYield interface {
	lua.LValue
	HandleResult(l *lua.LState, data any, err error) []lua.LValue
}

// ModuleDef is the complete module definition.
type ModuleDef struct {
	Name        string
	Description string
	Class       []string
	Default     bool
	Build       func() (*lua.LTable, []YieldType)
	BuildValue  func() (lua.LValue, []YieldType)
	Types       func() *types.TypeManifest

	once     sync.Once
	table    *lua.LTable
	value    lua.LValue
	yields   []YieldType
	manifest *types.TypeManifest
}

// Load loads the module into LState.
func (m *ModuleDef) Load(l *lua.LState) []YieldType {
	m.once.Do(func() {
		if m.BuildValue != nil {
			m.value, m.yields = m.BuildValue()
		} else if m.Build != nil {
			m.table, m.yields = m.Build()
			m.value = m.table
		}
		if m.Types != nil {
			m.manifest = m.Types()
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
func (m *ModuleDef) Info() ModuleInfo {
	return ModuleInfo{Name: m.Name, Description: m.Description, Class: m.Class}
}

// Register initializes the module and returns its registration.
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
		if m.Types != nil {
			m.manifest = m.Types()
		}
	})
	return &Registration{Table: m.table, YieldTypes: m.yields}
}

// Manifest returns the type manifest for this module.
// Returns nil if the module has no type definitions.
func (m *ModuleDef) Manifest() *types.TypeManifest {
	return m.manifest
}

// Loader initializes the module and pushes it onto the stack.
func (m *ModuleDef) Loader(l *lua.LState) int {
	m.Register(l)
	if m.value != nil {
		l.Push(m.value)
	} else {
		l.Push(m.table)
	}
	return 1
}

// LoadModule loads a module into the LState.
func LoadModule(l *lua.LState, m Module) []YieldType {
	info := m.Info()
	reg := m.Register(l)

	l.SetGlobal(info.Name, reg.Table)

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
