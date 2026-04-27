// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
)

// CompiledProto represents a compiled Lua prototype with its name
type CompiledProto struct {
	Proto *glua.FunctionProto
	Node  *Node
	Name  string
}

// CompiledMain holds the compiled versions of the main function and its dependencies
type CompiledMain struct {
	Main         *glua.FunctionProto
	FuncName     string
	Dependencies []CompiledProto
	Preloaded    []CompiledProto
}

// CompileFn compiles a node against the graph snapshot used for the build.
type CompileFn func(memGraph *MemoryGraph, node *Node) (*glua.FunctionProto, error)

type compiledProtoCacheKey struct {
	ID  registry.ID
	Tag string
}

type compiledMainCacheKey struct {
	ID      registry.ID
	Tag     string
	Options string
}

// Compiler handles the compilation of Lua code and caches results
type Compiler struct {
	protoCache *lru.Cache[compiledProtoCacheKey, *glua.FunctionProto]
	mainCache  *lru.Cache[compiledMainCacheKey, *CompiledMain]
	protoByID  map[registry.ID]map[compiledProtoCacheKey]struct{}
	mainByID   map[registry.ID]map[compiledMainCacheKey]struct{}
	compileFn  CompileFn
	indexMu    sync.Mutex
}

// NewCompiler returns a new Compiler with caches
func NewCompiler(
	compileFn CompileFn,
	protoCacheCapacity int,
	mainCacheCapacity int,
) *Compiler {
	c := &Compiler{
		protoByID: make(map[registry.ID]map[compiledProtoCacheKey]struct{}),
		mainByID:  make(map[registry.ID]map[compiledMainCacheKey]struct{}),
		compileFn: compileFn,
	}

	c.protoCache = lru.New[compiledProtoCacheKey, *glua.FunctionProto](
		lru.WithCapacity(protoCacheCapacity),
		lru.WithOnEvict(func(key compiledProtoCacheKey, _ *glua.FunctionProto) {
			c.removeProtoKey(key)
		}),
	)
	c.mainCache = lru.New[compiledMainCacheKey, *CompiledMain](
		lru.WithCapacity(mainCacheCapacity),
		lru.WithOnEvict(func(key compiledMainCacheKey, _ *CompiledMain) {
			c.removeMainKey(key)
		}),
	)

	return c
}

// getCompiledProto retrieves a node's compiled function prototype from cache or compiles it
func (c *Compiler) getCompiledProto(memGraph *MemoryGraph, node *Node, memo map[registry.ID]string) (*glua.FunctionProto, error) {
	if node.Kind == lua.ModuleKind {
		return nil, ErrModuleNotCompiled
	}

	tag, err := runtimeFingerprintMemo(memGraph, node.ID, memo)
	if err != nil {
		return nil, err
	}
	key := compiledProtoCacheKey{ID: node.ID, Tag: tag}

	if proto, ok := c.protoCache.Get(key); ok {
		return proto, nil
	}

	compiled, err := c.compileFn(memGraph, node)
	if err != nil {
		return nil, err
	}

	_ = c.protoCache.Set(key, compiled)
	c.recordProtoKey(key)
	return compiled, nil
}

// Invalidate removes entries from both caches for the given IDs
func (c *Compiler) Invalidate(ids []registry.ID) {
	c.indexMu.Lock()
	protoKeys := make([]compiledProtoCacheKey, 0)
	mainKeys := make([]compiledMainCacheKey, 0)
	for _, id := range ids {
		for key := range c.protoByID[id] {
			protoKeys = append(protoKeys, key)
		}
		delete(c.protoByID, id)
		for key := range c.mainByID[id] {
			mainKeys = append(mainKeys, key)
		}
		delete(c.mainByID, id)
	}
	c.indexMu.Unlock()

	for _, key := range protoKeys {
		c.protoCache.Delete(key)
	}
	for _, key := range mainKeys {
		c.mainCache.Delete(key)
	}
}

// SetProto injects a precompiled prototype into the cache.
func (c *Compiler) SetProto(id registry.ID, tag string, proto *glua.FunctionProto) {
	key := compiledProtoCacheKey{ID: id, Tag: tag}
	_ = c.protoCache.Set(key, proto)
	c.recordProtoKey(key)
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

	memo := make(map[registry.ID]string)
	tag, err := runtimeFingerprintMemo(memGraph, entrypoint, memo)
	if err != nil {
		return nil, err
	}
	key := compiledMainCacheKey{
		ID:      entrypoint,
		Tag:     tag,
		Options: BuildOptionsFingerprint(options),
	}

	if cached, ok := c.mainCache.Get(key); ok {
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

	// Compile dependencies
	for _, dep := range rt.Dependencies {
		if dep.Node.Kind == lua.ModuleKind {
			compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
				Name: dep.Name,
				Node: dep.Node,
			})
			continue
		}

		proto, err := c.getCompiledProto(memGraph, dep.Node, memo)
		if err != nil {
			return nil, NewCompileError(dep.Node.ID, err)
		}

		compiled.Dependencies = append(compiled.Dependencies, CompiledProto{
			Name:  dep.Name,
			Proto: proto,
			Node:  dep.Node,
		})
	}

	// Compile main node
	mainProto, err := c.getCompiledProto(memGraph, rt.Main, memo)
	if err != nil {
		return nil, NewCompileError(rt.Main.ID, err)
	}

	compiled.Main = mainProto

	_ = c.mainCache.Set(key, compiled)
	c.recordMainKey(key)

	return compiled, nil
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

func (c *Compiler) recordProtoKey(key compiledProtoCacheKey) {
	c.indexMu.Lock()
	defer c.indexMu.Unlock()

	keys := c.protoByID[key.ID]
	if keys == nil {
		keys = make(map[compiledProtoCacheKey]struct{})
		c.protoByID[key.ID] = keys
	}
	keys[key] = struct{}{}
}

func (c *Compiler) removeProtoKey(key compiledProtoCacheKey) {
	c.indexMu.Lock()
	defer c.indexMu.Unlock()

	keys := c.protoByID[key.ID]
	if keys == nil {
		return
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(c.protoByID, key.ID)
	}
}

func (c *Compiler) recordMainKey(key compiledMainCacheKey) {
	c.indexMu.Lock()
	defer c.indexMu.Unlock()

	keys := c.mainByID[key.ID]
	if keys == nil {
		keys = make(map[compiledMainCacheKey]struct{})
		c.mainByID[key.ID] = keys
	}
	keys[key] = struct{}{}
}

func (c *Compiler) removeMainKey(key compiledMainCacheKey) {
	c.indexMu.Lock()
	defer c.indexMu.Unlock()

	keys := c.mainByID[key.ID]
	if keys == nil {
		return
	}
	delete(keys, key)
	if len(keys) == 0 {
		delete(c.mainByID, key.ID)
	}
}
