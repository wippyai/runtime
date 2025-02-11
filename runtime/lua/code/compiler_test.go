package code

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	glua "github.com/yuin/gopher-lua"
	"testing"
)

// Mock compiler function for testing
type mockCompileFn struct {
	calls   map[string]int
	results map[string]*glua.FunctionProto
	errors  map[string]error
}

func newMockCompileFn() *mockCompileFn {
	return &mockCompileFn{
		calls:   make(map[string]int),
		results: make(map[string]*glua.FunctionProto),
		errors:  make(map[string]error),
	}
}

func (m *mockCompileFn) compile(n *Node) (*glua.FunctionProto, error) {
	m.calls[n.ID.String()]++
	if err, ok := m.errors[n.ID.String()]; ok {
		return nil, err
	}
	return m.results[n.ID.String()], nil
}

// Test compilation of regular Lua functions
func TestCompiler_CompileLuaFunction(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	node := &Node{
		ID:     registry.ID{Name: "test"},
		Kind:   "function.lua",
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	mock.results[node.ID.String()] = &glua.FunctionProto{}

	proto, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.NotNil(t, proto)
	assert.Equal(t, 1, mock.calls[node.ID.String()])

	// Test cache hit
	proto2, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.Equal(t, proto, proto2)
	assert.Equal(t, 1, mock.calls[node.ID.String()], "Should use cached version")
}

// Test that modules are not compiled
func TestCompiler_ModuleNotCompiled(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	moduleNode := &Node{
		ID:     registry.ID{Name: "testMod"},
		Kind:   lua.KindModule,
		Module: &dummyModule{name: "test"},
	}

	proto, err := compiler.getCompiledProto(moduleNode)
	require.NoError(t, err)
	assert.Nil(t, proto, "Modules should not be compiled")
	assert.Empty(t, mock.calls, "Compile should not be called for modules")
}

// Test that modules cannot be main nodes
func TestCompiler_ModuleCannotBeMain(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	moduleNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   lua.KindModule,
		Module: &dummyModule{name: "test"},
	}

	require.NoError(t, compiler.memGraph.AddNode(moduleNode))

	_, err := compiler.Compile(moduleNode.ID, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "main node cannot be a module")
}

// Test compilation with mixed Lua and module dependencies
func TestCompiler_MixedDependencies(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	mainNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   "function.lua",
		Source: "function main() return dep() end",
		Method: "test",
	}
	luaDepNode := &Node{
		ID:     registry.ID{Name: "dep"},
		Kind:   "function.lua",
		Source: "function dep() return 'hello' end",
		Method: "test",
	}
	moduleNode := &Node{
		ID:     registry.ID{Name: "mod"},
		Kind:   lua.KindModule,
		Module: &dummyModule{name: "test"},
	}

	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}
	mock.results[luaDepNode.ID.String()] = &glua.FunctionProto{}

	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(luaDepNode))
	require.NoError(t, mg.AddNode(moduleNode))
	require.NoError(t, mg.AddDependency(mainNode.ID, luaDepNode.ID, "dep"))
	require.NoError(t, mg.AddDependency(mainNode.ID, moduleNode.ID, "mod"))

	compiled, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)
	assert.NotNil(t, compiled.Main)
	assert.Len(t, compiled.Dependencies, 2)

	// Verify one dependency is compiled and one is not
	var foundCompiledDep, foundModule bool
	for _, dep := range compiled.Dependencies {
		if dep.Node.Kind == lua.KindModule {
			assert.Nil(t, dep.Proto)
			foundModule = true
		} else {
			assert.NotNil(t, dep.Proto)
			foundCompiledDep = true
		}
	}
	assert.True(t, foundCompiledDep, "Should have a compiled Lua dependency")
	assert.True(t, foundModule, "Should have an uncompiled module dependency")
}

// Test cache invalidation
func TestCompiler_Invalidation(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	mainNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}
	depNode := &Node{
		ID:     registry.ID{Name: "dep"},
		Kind:   "function.lua",
		Source: "function dep() end",
		Method: "test",
	}

	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}
	mock.results[depNode.ID.String()] = &glua.FunctionProto{}

	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(depNode))
	require.NoError(t, mg.AddDependency(mainNode.ID, depNode.ID, "dep"))

	// 1. Initial fresh compilation
	compiled1, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)

	// 2. Get from cache - should reuse
	compiled2, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, compiled1, compiled2, "Should get same result from cache")

	// 3. Invalidate both nodes
	compiler.Invalidate([]registry.ID{mainNode.ID, depNode.ID})

	// 4. Fresh compilation after invalidation - should recompile
	compiled3, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, compiled1, compiled3, "Should get same result after invalidation")

	// Verify total calls - should be 4 (2 initial + 2 after invalidate)
	totalCalls := 0
	for _, count := range mock.calls {
		totalCalls += count
	}
	assert.Equal(t, 4, totalCalls, "Should compile both nodes twice")
}

// Test preloaded dependencies
func TestCompiler_PreloadedDependencies(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	mainNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}
	preloadedLua := &Node{
		ID:     registry.ID{Name: "preluaded_lua"},
		Kind:   "function.lua",
		Source: "function preloaded() end",
		Method: "test",
	}
	preloadedModule := &Node{
		ID:     registry.ID{Name: "preloaded_mod"},
		Kind:   lua.KindModule,
		Module: &dummyModule{name: "preloaded"},
	}

	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}
	mock.results[preloadedLua.ID.String()] = &glua.FunctionProto{}

	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(preloadedLua))
	require.NoError(t, mg.AddNode(preloadedModule))

	options := NewBuildOptions().WithPreloaded(
		Dependency{Name: "pre_lua", Node: preloadedLua},
		Dependency{Name: "pre_mod", Node: preloadedModule},
	)

	compiled, err := compiler.Compile(mainNode.ID, options)
	require.NoError(t, err)
	assert.NotNil(t, compiled.Main)
	assert.Len(t, compiled.Dependencies, 2)

	// Verify preloaded dependencies are included
	var foundPreloadedLua, foundPreloadedModule bool
	for _, dep := range compiled.Dependencies {
		if dep.Name == "pre_lua" {
			assert.NotNil(t, dep.Proto)
			foundPreloadedLua = true
		}
		if dep.Name == "pre_mod" {
			assert.Nil(t, dep.Proto)
			assert.NotNil(t, dep.Node.Module)
			foundPreloadedModule = true
		}
	}
	assert.True(t, foundPreloadedLua, "Should have preloaded Lua dependency")
	assert.True(t, foundPreloadedModule, "Should have preloaded module dependency")
}
