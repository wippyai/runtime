package code

import (
	"github.com/ponyruntime/pony/api/registry"
	luapi "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

// NamedProto represents a named Lua function prototype.
type NamedProto struct {
	Name  string
	Proto *lua.FunctionProto
}

// Node represents a code unit in the dependency graph.
// A node may contain either a Lua prototype (Proto) or a module reference.
type Node struct {
	ID     registry.ID
	Kind   registry.Kind
	Source string
	Method string

	Proto  *lua.FunctionProto
	Module *luapi.Module
}

// Runtime aggregates a main function prototype, its method,
// all dependency prototypes, and any required modules.
type Runtime struct {
	Main      NamedProto
	Method    string
	DepProtos []NamedProto
	Modules   []luapi.Module
}

// CodeGraph represents a directed dependency graph for code nodes.
// Each node is a code unit and an edge from node A to node B indicates that
// A depends on B.
type CodeGraph interface {
	// AddNode inserts a new code node into the graph.
	// Returns an error if a node with the same ID already exists.
	AddNode(n Node) error

	// RemoveNode deletes the node identified by id along with all its dependency edges.
	// Returns an error if the node is not found.
	RemoveNode(id registry.ID) error

	// AddDependency creates a dependency edge from the node with ID 'from'
	// to the node with ID 'to'. This signifies that 'from' depends on 'to'.
	// Returns an error if either node does not exist.
	AddDependency(from, to registry.ID) error

	// RemoveDependency removes the dependency edge from 'from' to 'to'.
	// Returns an error if the dependency edge does not exist.
	RemoveDependency(from, to registry.ID) error

	// GetNode retrieves the code node associated with the given id.
	GetNode(id registry.ID) (Node, error)

	// GetDependencies returns a slice of nodes that the node identified by id depends on.
	// Returns an error if the node does not exist.
	GetDependencies(id registry.ID) ([]Node, error)

	// GetDependents returns a slice of nodes that depend on the node identified by id.
	// Returns an error if the node does not exist.
	GetDependents(id registry.ID) ([]Node, error)

	// DependencyLevels returns a topologically sorted grouping of nodes.
	// Each inner slice represents a level where nodes only depend on nodes from previous levels.
	// If a cycle is detected, an error is returned.
	DependencyLevels() ([][]Node, error)

	// BuildRuntime resolves dependencies starting from the entrypoint node (identified by id)
	// and constructs a Runtime configuration. It returns an error if dependency resolution fails.
	BuildRuntime(entrypoint registry.ID) (Runtime, error)
}
