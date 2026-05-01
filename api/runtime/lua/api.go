// SPDX-License-Identifier: MPL-2.0

// Package lua provides Lua runtime integration types.
package lua

import (
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/io"
)

const (
	System                event.System = "lua"
	InvalidateNodes       event.Kind   = "lua.reset_code"
	InvalidateNodesAccept event.Kind   = "lua.reset_code.accept"
	InvalidateNodesReject event.Kind   = "lua.reset_code.reject"
	InvalidateNodesResult event.Kind   = "lua.reset_code.(accept|reject)"
)

// InvalidateNode identifies a Lua code node that may need runtime invalidation.
type InvalidateNode struct {
	ID   registry.ID
	Kind registry.Kind
}

// InvalidateNodesRequest carries an optional acknowledgement prefix. When set,
// component handlers acknowledge every matching node at AckPrefix + "/" + node ID.
type InvalidateNodesRequest struct {
	AckPrefix string
	Nodes     []InvalidateNode
}

// Module class constants
const (
	ClassDeterministic    = "deterministic"
	ClassNondeterministic = "nondeterministic"
	ClassIO               = "io"
	ClassNetwork          = "network"
	ClassEncoding         = "encoding"
	ClassTime             = "time"
	ClassProcess          = "process"
	ClassSecurity         = "security"
	ClassStorage          = "storage"
	ClassWorkflow         = "workflow"
)

// ModuleInfo contains metadata about a Lua module
type ModuleInfo struct {
	Name        string
	Description string
	Class       []string
}

// YieldType describes a yield and how to handle it.
type YieldType struct {
	Sample any
	CmdID  dispatcher.CommandID
}

// Module is the interface for Lua modules (used by code graph).
type Module interface {
	Info() ModuleInfo
	Value() lua.LValue
	Yields() []YieldType
}

// Registration contains module configuration.
type Registration struct {
	Table      *lua.LTable
	YieldTypes []YieldType
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

// ModuleDef is a module definition struct. Caching and loading in engine.
type ModuleDef struct {
	Build       func() (*lua.LTable, []YieldType)
	BuildValue  func() (lua.LValue, []YieldType)
	Types       func() *io.Manifest
	Name        string
	Description string
	Class       []string
}

// Info returns module metadata.
func (m *ModuleDef) Info() ModuleInfo {
	return ModuleInfo{Name: m.Name, Description: m.Description, Class: m.Class}
}
