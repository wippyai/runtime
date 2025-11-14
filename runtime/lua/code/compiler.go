package code

import (
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
	glua "github.com/yuin/gopher-lua"
)

// CompiledProto represents a compiled Lua prototype with its name
type CompiledProto struct {
	Name  string
	Proto *glua.FunctionProto
	Node  *Node
}

// CompiledMain holds the compiled versions of the main function and its dependencies
type CompiledMain struct {
	Main         *glua.FunctionProto
	FuncName     string
	Dependencies []CompiledProto
	Preloaded    []CompiledProto
}

// Compiler handles the compilation of Lua code and caches results
type Compiler struct {
	protoCache *lru.Cache[registry.ID, *glua.FunctionProto] // Cache for individual function prototypes
	mainCache  *lru.Cache[registry.ID, *CompiledMain]       // Cache for compiled mains
	compileFn  func(*Node) (*glua.FunctionProto, error)
}

// NewCompiler returns a new Compiler with a MemoryGraph and caches
func NewCompiler(
	compileFn func(*Node) (*glua.FunctionProto, error),
	protoCacheCapacity int,
	mainCacheCapacity int,
) *Compiler {
	return &Compiler{
		protoCache: lru.New[registry.ID, *glua.FunctionProto](
			lru.WithCapacity(protoCacheCapacity),
		),
		mainCache: lru.New[registry.ID, *CompiledMain](
			lru.WithCapacity(mainCacheCapacity),
		),
		compileFn: compileFn,
	}
}

// getCompiledProto retrieves a node's compiled function prototype from cache or compiles it
// Returns nil without error for module nodes
func (c *Compiler) getCompiledProto(node *Node) (*glua.FunctionProto, error) {
	// Modules don't need compilation
	if node.Kind == lua.KindModule {
		return nil, errors.New("module nodes are not compiled")
	}

	if proto, ok := c.protoCache.Get(node.ID); ok {
		return proto, nil
	}

	compiled, err := c.compileFn(node)
	if err != nil {
		return nil, err
	}

	c.protoCache.Set(node.ID, compiled)
	return compiled, nil
}

// Invalidate removes entries from both caches for the given IDs
func (c *Compiler) Invalidate(ids []registry.ID) {
	for _, id := range ids {
		c.protoCache.Delete(id)
		c.mainCache.Delete(id)
	}
}

// Compile builds and compiles a main function and its dependencies
func (c *Compiler) Compile(
	memGraph *MemoryGraph,
	entrypoint registry.ID,
	options *BuildOptions,
) (*CompiledMain, error) {
	if options == nil {
		options = NewBuildOptions()
	}

	// Check main cache first
	if cached, ok := c.mainCache.Get(entrypoint); ok {
		return cached, nil
	}

	// Build the runtime configuration
	rt, err := memGraph.Build(entrypoint)
	if err != nil {
		return nil, err
	}

	// Validate nodes against build options
	nodes := make(map[registry.ID]*Node)
	for _, dep := range rt.Dependencies {
		nodes[dep.Node.ID] = dep.Node
	}
	nodes[rt.Main.ID] = rt.Main

	if err := options.Validate(nodes); err != nil {
		return nil, fmt.Errorf("build options validation failed: %w", err)
	}

	// Compile main node
	mainProto, err := c.getCompiledProto(rt.Main)
	if err != nil {
		return nil, fmt.Errorf("failed to compile main node: %w", err)
	}

	compiled := &CompiledMain{}
	compiled.Main = mainProto
	compiled.FuncName = rt.Main.Method

	for _, pre := range options.Preloaded {
		main, pErr := c.preloadModule(memGraph, pre, compiled)
		if pErr != nil {
			return main, pErr
		}
	}

	// Compile regular dependencies
	for _, dep := range rt.Dependencies {
		if dep.Node.Kind == lua.KindModule {
			compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
				Name: dep.Name,
				Node: dep.Node,
			})
			continue
		}

		proto, err := c.getCompiledProto(dep.Node)
		if err != nil {
			return nil, fmt.Errorf("failed to compile dependency node %v: %w", dep.Node.ID, err)
		}

		compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
			Name:  dep.Name,
			Proto: proto,
			Node:  dep.Node,
		})
	}

	// Cache the compiled main

	c.mainCache.Set(entrypoint, compiled)

	return compiled, nil
}

//nolint:unparam // ok for now
func (c *Compiler) preloadModule(memGraph *MemoryGraph, pre Preload, compiled *CompiledMain) (*CompiledMain, error) {
	node, err := memGraph.GetNode(pre.ModuleID)
	if err != nil {
		return nil, fmt.Errorf("failed to preload %v: %w", pre, err)
	}

	// we can only preload modules
	compiled.Preloaded = append(compiled.Preloaded, CompiledProto{
		Name: pre.Name,
		Node: node,
	})
	return nil, nil
}
