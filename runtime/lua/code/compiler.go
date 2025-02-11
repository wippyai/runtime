package lua

import (
	"fmt"
	lru "github.com/ponyruntime/pony/internal/cache"

	"github.com/ponyruntime/pony/api/registry"
	runtime "github.com/ponyruntime/pony/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
)

const defaultCapacity = 9000

// CompiledProto represents a compiled Lua prototype with its optional alias
type CompiledProto struct {
	Alias string
	Proto *glua.FunctionProto
}

// CompiledMain holds the compiled versions of the main function,
// its dependencies, and any required modules
type CompiledMain struct {
	// The compiled main function prototype
	Main *glua.FunctionProto

	// All compiled dependency prototypes with their aliases
	Dependencies []CompiledProto

	// Required modules (not compiled, just referenced)
	Modules []*runtime.Module
}

// NewCompiledMain creates a new CompiledMain instance
func NewCompiledMain() *CompiledMain {
	return &CompiledMain{
		Dependencies: make([]CompiledProto, 0),
		Modules:      make([]*runtime.Module, 0),
	}
}

// Compiler composes a MemoryGraph with an LRU cache for compiled nodes.
// compileFn is injected from outside to compile Lua Source code into a *glua.FunctionProto.
type Compiler struct {
	memGraph  *MemoryGraph
	cache     *lru.Cache[string, *glua.FunctionProto]
	compileFn func(source string) (*glua.FunctionProto, error)
}

// NewCompiler returns a new Compiler with a MemoryGraph and an LRU cache.
// 'compileFn' is provided by the caller and is used to compile Lua Source code.
func NewCompiler(
	compileFn func(source string) (*glua.FunctionProto, error),
) *Compiler {
	return &Compiler{
		memGraph:  NewMemoryGraph(),
		cache:     lru.New[string, *glua.FunctionProto](lru.WithCapacity(defaultCapacity)),
		compileFn: compileFn,
	}
}

// getCompiledProto retrieves a node's compiled function prototype from cache or compiles it.
func (c *Compiler) getCompiledProto(node *Node) (*glua.FunctionProto, error) {
	if proto, ok := c.cache.Get(node.Hash); ok {
		return proto, nil
	}

	compiled, err := c.compileFn(node.Source)
	if err != nil {
		return nil, err
	}
	c.cache.Set(node.Hash, compiled)
	return compiled, nil
}

// Compile builds the runtime using MemoryGraph.Build, compiles the main node and all its dependencies,
// and returns a CompiledMain containing all compiled components and required modules.
func (c *Compiler) Compile(entrypoint registry.ID) (*CompiledMain, error) {
	// Build the runtime configuration
	rt, err := c.memGraph.Build(entrypoint)
	if err != nil {
		return nil, err
	}

	// Create new CompiledMain instance
	compiled := NewCompiledMain()

	// Compile main node
	mainProto, err := c.getCompiledProto(rt.Main)
	if err != nil {
		return nil, fmt.Errorf("failed to compile main node: %w", err)
	}
	compiled.Main = mainProto

	// Compile all dependencies
	for _, aliased := range rt.DepProtos {
		depProto, err := c.getCompiledProto(aliased.Node)
		if err != nil {
			return nil, fmt.Errorf("failed to compile dependency node %v: %w", aliased.Node.ID, err)
		}
		compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
			Alias: aliased.Alias,
			Proto: depProto,
		})
	}

	// Copy over the modules (not compiled)
	compiled.Modules = rt.Modules

	return compiled, nil
}
