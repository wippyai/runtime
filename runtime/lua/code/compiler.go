package code

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
	glua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/types"
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

// CompileFn compiles a node with its imports (for type checking).
type CompileFn func(node *Node, imports map[string]*types.TypeManifest) (*glua.FunctionProto, error)

// Compiler handles the compilation of Lua code and caches results
type Compiler struct {
	protoCache *lru.Cache[registry.ID, *glua.FunctionProto]
	mainCache  *lru.Cache[registry.ID, *CompiledMain]
	compileFn  CompileFn
}

// NewCompiler returns a new Compiler with caches
func NewCompiler(
	compileFn CompileFn,
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
func (c *Compiler) getCompiledProto(node *Node, imports map[string]*types.TypeManifest) (*glua.FunctionProto, error) {
	if node.Kind == lua.ModuleKind {
		return nil, ErrModuleNotCompiled
	}

	if proto, ok := c.protoCache.Get(node.ID); ok {
		return proto, nil
	}

	compiled, err := c.compileFn(node, imports)
	if err != nil {
		return nil, err
	}

	_ = c.protoCache.Set(node.ID, compiled)
	return compiled, nil
}

// Invalidate removes entries from both caches for the given IDs
func (c *Compiler) Invalidate(ids []registry.ID) {
	for _, id := range ids {
		c.protoCache.Delete(id)
		c.mainCache.Delete(id)
	}
}

// SetProto injects a precompiled prototype into the cache.
func (c *Compiler) SetProto(id registry.ID, proto *glua.FunctionProto) {
	_ = c.protoCache.Set(id, proto)
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

	if cached, ok := c.mainCache.Get(entrypoint); ok {
		return cached, nil
	}

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
		return nil, err
	}

	compiled := &CompiledMain{}
	compiled.FuncName = rt.Main.Method

	for _, pre := range options.Preloaded {
		if err := c.preloadModule(memGraph, pre, compiled); err != nil {
			return nil, err
		}
	}

	// Compile dependencies FIRST (so their manifests are available for main)
	for _, dep := range rt.Dependencies {
		if dep.Node.Kind == lua.ModuleKind {
			compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
				Name: dep.Name,
				Node: dep.Node,
			})
			continue
		}

		// Get this dependency's own imports
		depDeps, _ := memGraph.GetDependenciesWithAliases(dep.Node.ID)
		depImports := buildImportsFromDependencies(depDeps)

		proto, err := c.getCompiledProto(dep.Node, depImports)
		if err != nil {
			return nil, NewCompileError(dep.Node.ID, err)
		}

		compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
			Name:  dep.Name,
			Proto: proto,
			Node:  dep.Node,
		})
	}

	// Build imports for main node from its now-compiled dependencies
	mainImports := buildImportsFromDependencies(rt.Dependencies)

	// Compile main node
	mainProto, err := c.getCompiledProto(rt.Main, mainImports)
	if err != nil {
		return nil, NewCompileError(rt.Main.ID, err)
	}

	compiled.Main = mainProto

	_ = c.mainCache.Set(entrypoint, compiled)

	return compiled, nil
}

// buildImportsFromDependencies extracts type manifests from module dependencies
func buildImportsFromDependencies(deps []Dependency) map[string]*types.TypeManifest {
	imports := make(map[string]*types.TypeManifest)
	for _, dep := range deps {
		if dep.Node == nil {
			continue
		}

		// Check for Lua source file manifest (stored after type checking)
		if dep.Node.Manifest != nil {
			imports[dep.Name] = dep.Node.Manifest
			continue
		}

		// Check for Go module manifest
		if dep.Node.Module == nil {
			continue
		}
		// Get Types from module if available
		if dep.Node.Module.Types != nil {
			if manifest := dep.Node.Module.Types(); manifest != nil {
				imports[dep.Name] = manifest
			}
		}
	}
	return imports
}

func (c *Compiler) preloadModule(memGraph *MemoryGraph, pre Preload, compiled *CompiledMain) error {
	node, err := memGraph.GetNode(pre.ModuleID)
	if err != nil {
		return err
	}

	compiled.Preloaded = append(compiled.Preloaded, CompiledProto{
		Name: pre.Name,
		Node: node,
	})
	return nil
}
