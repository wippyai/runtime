package lua

import (
	"github.com/ponyruntime/pony/api/registry"
)

type (
	// AliasedNode represents a named Lua function prototype.
	AliasedNode struct {
		Alias string
		Node  *Node
	}

	// Node represents a code unit in the dependency graph.
	// A node may contain either a Lua prototype (Proto) or a module reference.
	Node struct {
		ID     registry.ID
		Kind   registry.Kind
		Source string
		Method string
		Module *Module
	}

	Edge struct {
		Alias string
	}

	// Main aggregates a main function prototype, its method,
	// all dependency prototypes, and any required modules.
	Main struct {
		Main      *Node
		DepProtos []AliasedNode
		Modules   []*Module
	}
)
