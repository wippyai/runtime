// SPDX-License-Identifier: MPL-2.0

package code

import (
	"sync"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
)

// CompiledProto represents a compiled Lua prototype with its name
type CompiledProto struct {
	Proto *glua.FunctionProto
	Node  *Node
	Name  string
}

// CompiledMain holds the compiled versions of the main function and its dependencies
type CompiledMain struct {
	Main *glua.FunctionProto
	// Imports maps each code node (entrypoint and libraries) to its directly
	// declared imports, used to build per-chunk scoped environments.
	Imports      map[registry.ID][]Import
	FuncName     string
	MainID       registry.ID
	Preloaded    []CompiledProto
	Dependencies []CompiledProto
}

// buildMemo shares graph-derived fingerprints across one complete build.
// Without this, deep dependency chains are traversed again for every node.
type buildMemo struct {
	runtime       map[registry.ID]string
	compile       map[registry.ID]string
	compileMeta   map[registry.ID][]cache.DepMeta
	typecheck     map[registry.ID]string
	typecheckMeta map[registry.ID][]cache.DepMeta
}

func newBuildMemo() *buildMemo {
	return &buildMemo{
		runtime:       make(map[registry.ID]string),
		compile:       make(map[registry.ID]string),
		compileMeta:   make(map[registry.ID][]cache.DepMeta),
		typecheck:     make(map[registry.ID]string),
		typecheckMeta: make(map[registry.ID][]cache.DepMeta),
	}
}

// CompileFn compiles a node against the graph snapshot used for the build.
type CompileFn func(memGraph *MemoryGraph, node *Node) (*glua.FunctionProto, error)

type compileMemoFn func(memGraph *MemoryGraph, node *Node, memo *buildMemo) (*glua.FunctionProto, error)

type retainedProtoKey struct {
	ID  registry.ID
	Tag string
}

type retainedMainKey struct {
	ID      registry.ID
	Tag     string
	Options string
}

// Compiler retains compiled code until its owning registry nodes are invalidated.
type Compiler struct {
	retainedProtos map[retainedProtoKey]*glua.FunctionProto
	retainedMains  map[retainedMainKey]*CompiledMain
	protosByNode   map[registry.ID]map[retainedProtoKey]struct{}
	mainsByNode    map[registry.ID]map[retainedMainKey]struct{}
	compileFn      CompileFn
	compileMemoFn  compileMemoFn
	retainedMu     sync.RWMutex
}

// NewCompiler returns a compiler with lifecycle-owned retained code.
func NewCompiler(compileFn CompileFn) *Compiler {
	compiler := newCompiler()
	compiler.compileFn = compileFn
	return compiler
}

func newCompilerWithMemo(compileFn compileMemoFn) *Compiler {
	compiler := newCompiler()
	compiler.compileMemoFn = compileFn
	return compiler
}

func newCompiler() *Compiler {
	return &Compiler{
		retainedProtos: make(map[retainedProtoKey]*glua.FunctionProto),
		retainedMains:  make(map[retainedMainKey]*CompiledMain),
		protosByNode:   make(map[registry.ID]map[retainedProtoKey]struct{}),
		mainsByNode:    make(map[registry.ID]map[retainedMainKey]struct{}),
	}
}

// getCompiledProto retrieves retained code or compiles it for the active node version.
func (c *Compiler) getCompiledProto(memGraph *MemoryGraph, node *Node, memo *buildMemo) (*glua.FunctionProto, error) {
	if node.Kind == lua.ModuleKind {
		return nil, ErrModuleNotCompiled
	}

	tag, err := runtimeFingerprintMemo(memGraph, node.ID, memo.runtime)
	if err != nil {
		return nil, err
	}
	key := retainedProtoKey{ID: node.ID, Tag: tag}

	c.retainedMu.RLock()
	proto, ok := c.retainedProtos[key]
	c.retainedMu.RUnlock()
	if ok {
		return proto, nil
	}

	var compiled *glua.FunctionProto
	if c.compileMemoFn != nil {
		compiled, err = c.compileMemoFn(memGraph, node, memo)
	} else {
		compiled, err = c.compileFn(memGraph, node)
	}
	if err != nil {
		return nil, err
	}

	return c.retainProto(key, compiled), nil
}

// Invalidate releases retained code owned by the given registry nodes.
func (c *Compiler) Invalidate(ids []registry.ID) {
	c.retainedMu.Lock()
	defer c.retainedMu.Unlock()
	for _, id := range ids {
		for key := range c.protosByNode[id] {
			delete(c.retainedProtos, key)
		}
		delete(c.protosByNode, id)
		for key := range c.mainsByNode[id] {
			delete(c.retainedMains, key)
		}
		delete(c.mainsByNode, id)
	}
}

// SetProto injects a precompiled prototype into the cache.
func (c *Compiler) SetProto(id registry.ID, tag string, proto *glua.FunctionProto) {
	key := retainedProtoKey{ID: id, Tag: tag}
	c.retainedMu.Lock()
	defer c.retainedMu.Unlock()
	c.retainedProtos[key] = proto
	c.recordProtoKeyLocked(key)
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

	memo := newBuildMemo()
	tag, err := runtimeFingerprintMemo(memGraph, entrypoint, memo.runtime)
	if err != nil {
		return nil, err
	}
	key := retainedMainKey{
		ID:      entrypoint,
		Tag:     tag,
		Options: BuildOptionsFingerprint(options),
	}

	c.retainedMu.RLock()
	cached, ok := c.retainedMains[key]
	c.retainedMu.RUnlock()
	if ok {
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
	compiled.MainID = rt.Main.ID
	compiled.Imports = rt.Imports

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

	return c.retainMain(key, compiled), nil
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

func (c *Compiler) retainProto(key retainedProtoKey, proto *glua.FunctionProto) *glua.FunctionProto {
	c.retainedMu.Lock()
	defer c.retainedMu.Unlock()
	if retained, ok := c.retainedProtos[key]; ok {
		return retained
	}
	c.retainedProtos[key] = proto
	c.recordProtoKeyLocked(key)
	return proto
}

func (c *Compiler) recordProtoKeyLocked(key retainedProtoKey) {
	keys := c.protosByNode[key.ID]
	if keys == nil {
		keys = make(map[retainedProtoKey]struct{})
		c.protosByNode[key.ID] = keys
	}
	keys[key] = struct{}{}
}

func (c *Compiler) retainMain(key retainedMainKey, compiled *CompiledMain) *CompiledMain {
	c.retainedMu.Lock()
	defer c.retainedMu.Unlock()
	if retained, ok := c.retainedMains[key]; ok {
		return retained
	}
	c.retainedMains[key] = compiled
	keys := c.mainsByNode[key.ID]
	if keys == nil {
		keys = make(map[retainedMainKey]struct{})
		c.mainsByNode[key.ID] = keys
	}
	keys[key] = struct{}{}
	return compiled
}
