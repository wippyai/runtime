package code

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
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
		ID:     registry.NewID("", "test"),
		Kind:   "function.lua",
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	mock.results[node.ID.String()] = &glua.FunctionProto{}

	proto, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.NotNil(t, proto)

	// Test cache hit
	proto2, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.Equal(t, proto, proto2)
}

// Test that modules are not compiled
func TestCompiler_ModuleNotCompiled(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)

	moduleNode := &Node{
		ID:     registry.NewID("", "moduleNotCompiled"),
		Kind:   lua.Module,
		Module: &dummyModule{name: "test"},
	}

	proto, err := compiler.getCompiledProto(moduleNode)
	require.Error(t, err)
	assert.Nil(t, proto, "Modules should not be compiled")
	assert.Empty(t, mock.calls, "Compile should not be called for modules")
}

// Test compilation with mixed Lua and module dependencies
func TestCompiler_MixedDependencies(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10, 10)
	memGraph := NewMemoryGraph()

	mainNode := &Node{
		ID:     registry.NewID("", "main"),
		Kind:   "function.lua",
		Source: "function main() return dep() end",
		Method: "test",
	}
	luaDepNode := &Node{
		ID:     registry.NewID("", "luaDep"),
		Kind:   "function.lua",
		Source: "function dep() return 'hello' end",
		Method: "test",
	}
	moduleNode := &Node{
		ID:     registry.NewID("", "module"),
		Kind:   lua.Module,
		Module: &dummyModule{name: "test"},
	}

	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}
	mock.results[luaDepNode.ID.String()] = &glua.FunctionProto{}

	require.NoError(t, memGraph.AddNode(mainNode))
	require.NoError(t, memGraph.AddNode(luaDepNode))
	require.NoError(t, memGraph.AddNode(moduleNode))
	require.NoError(t, memGraph.AddDependency(mainNode.ID, luaDepNode.ID, "dep"))
	require.NoError(t, memGraph.AddDependency(mainNode.ID, moduleNode.ID, "mod"))

	compiled, err := compiler.Compile(memGraph, mainNode.ID, nil)
	require.NoError(t, err)
	assert.NotNil(t, compiled.Main)
	assert.Len(t, compiled.Dependencies, 2)

	// Verify one dependency is compiled and one is not
	var foundCompiledDep, foundModule bool
	for _, dep := range compiled.Dependencies {
		if dep.Node.Kind == lua.Module {
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
	memGraph := NewMemoryGraph()

	mainNode := &Node{
		ID:     registry.NewID("", "mainInvalidation"),
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}
	depNode := &Node{
		ID:     registry.NewID("", "depInvalidation"),
		Kind:   "function.lua",
		Source: "function dep() end",
		Method: "test",
	}

	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}
	mock.results[depNode.ID.String()] = &glua.FunctionProto{}

	require.NoError(t, memGraph.AddNode(mainNode))
	require.NoError(t, memGraph.AddNode(depNode))
	require.NoError(t, memGraph.AddDependency(mainNode.ID, depNode.ID, "dep"))

	// 1. Initial fresh compilation
	compiled1, err := compiler.Compile(memGraph, mainNode.ID, nil)
	require.NoError(t, err)

	// 2. Get from cache - should reuse
	compiled2, err := compiler.Compile(memGraph, mainNode.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, compiled1, compiled2, "Should get same result from cache")

	// 3. Invalidate both nodes
	compiler.Invalidate([]registry.ID{mainNode.ID, depNode.ID})

	// 4. Fresh compilation after invalidation - should recompile
	compiled3, err := compiler.Compile(memGraph, mainNode.ID, nil)
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
	memGraph := NewMemoryGraph()

	// Spawn test nodes
	mainNode := &Node{
		ID:     registry.NewID("", "mainPreload"),
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}

	preloadedModule := &Node{
		ID:     registry.NewID("", "preloadedModule"),
		Kind:   lua.Module,
		Module: &dummyModule{name: "preloaded"},
	}

	// AddCleanup mock compilation result for main node
	mock.results[mainNode.ID.String()] = &glua.FunctionProto{}

	// AddCleanup nodes to graph
	require.NoError(t, memGraph.AddNode(mainNode))
	require.NoError(t, memGraph.AddNode(preloadedModule))

	// Spawn build options with preloaded module
	options := NewBuildOptions().WithPreloaded(
		Preload{
			Name:     "pre_mod",
			ModuleID: preloadedModule.ID,
		},
	)

	// Compile
	compiled, err := compiler.Compile(memGraph, mainNode.ID, options)
	require.NoError(t, err)
	require.NotNil(t, compiled)
	require.NotNil(t, compiled.Main)

	// Verify preloaded dependencies
	assert.Len(t, compiled.Dependencies, 0, "Should have no dependencies")
	assert.Len(t, compiled.Preloaded, 1, "Should have one preloaded dependency")

	dep := compiled.Preloaded[0]
	assert.Equal(t, "pre_mod", dep.Name)
	assert.Equal(t, preloadedModule, dep.Node)
	assert.NotNil(t, dep.Node.Module)
	assert.Equal(t, "preloaded", dep.Node.Module.Info().Name)
}

// testModule implements lua.ModuleV2 interface for testing
type testModule struct {
	name string
}

func (m *testModule) Loader(l *glua.LState) int {
	mod := l.CreateTable(0, 1)
	mod.RawSetString("test", l.NewFunction(func(l *glua.LState) int {
		l.Push(glua.LString("test"))
		return 1
	}))
	l.Push(mod)
	return 1
}

func (m *testModule) Info() lua.ModuleInfo {
	return lua.ModuleInfo{
		Name:        m.name,
		Description: "test module",
		Class:       []string{lua.ClassDeterministic},
	}
}

func (m *testModule) Register(_ *glua.LState) *lua.Registration {
	return nil
}

func TestNewCompiler(t *testing.T) {
	compileFn := func(*Node) (*glua.FunctionProto, error) { return &glua.FunctionProto{}, nil }
	compiler := NewCompiler(compileFn, 100, 200)

	assert.NotNil(t, compiler)
	assert.NotNil(t, compiler.protoCache)
	assert.NotNil(t, compiler.mainCache)
	assert.NotNil(t, compiler.compileFn)
}

func TestCompiler_GetCompiledProto(t *testing.T) {
	tests := []struct {
		name          string
		node          *Node
		compileFn     func(*Node) (*glua.FunctionProto, error)
		expectedProto *glua.FunctionProto
		expectError   bool
	}{
		{
			name: "Module node should return error",
			node: &Node{
				ID:   registry.NewID("", "moduleNode"),
				Kind: lua.Module,
			},
			compileFn:     func(*Node) (*glua.FunctionProto, error) { return &glua.FunctionProto{}, nil },
			expectedProto: nil,
			expectError:   true,
		},
		{
			name: "Cached proto should be returned",
			node: &Node{
				ID:   registry.NewID("", "cachedNode"),
				Kind: lua.Function,
			},
			compileFn: func(*Node) (*glua.FunctionProto, error) {
				return &glua.FunctionProto{}, nil
			},
			expectedProto: &glua.FunctionProto{},
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler(tt.compileFn, 100, 200)
			if !tt.expectError {
				compiler.protoCache.Set(tt.node.ID, tt.expectedProto)
			}

			proto, err := compiler.getCompiledProto(tt.node)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, proto)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedProto, proto)
			}
		})
	}
}

func TestCompiler_Invalidate(t *testing.T) {
	compiler := NewCompiler(func(*Node) (*glua.FunctionProto, error) { return &glua.FunctionProto{}, nil }, 100, 200)

	// Add some test data
	testID := registry.NewID("test", "id")
	compiler.protoCache.Set(testID, &glua.FunctionProto{})
	compiler.mainCache.Set(testID, &CompiledMain{})

	// Invalidate the test ID
	compiler.Invalidate([]registry.ID{testID})

	// Verify both caches are cleared
	_, ok := compiler.protoCache.Get(testID)
	assert.False(t, ok)
	_, ok = compiler.mainCache.Get(testID)
	assert.False(t, ok)
}

func TestCompiler_Compile(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*MemoryGraph) registry.ID
		options     *BuildOptions
		expectError bool
	}{
		{
			name: "Simple function compilation",
			setup: func(mg *MemoryGraph) registry.ID {
				node := &Node{
					ID:     registry.NewID("", "simpleFunc"),
					Kind:   lua.Function,
					Source: "return function() end",
					Method: "test",
				}
				require.NoError(t, mg.AddNode(node))
				return node.ID
			},
			options:     NewBuildOptions(),
			expectError: false,
		},
		{
			name: "Function with dependencies",
			setup: func(mg *MemoryGraph) registry.ID {
				// Add dependency
				dep := &Node{
					ID:     registry.NewID("", "depFunc"),
					Kind:   lua.Function,
					Source: "return function() end",
					Method: "dep",
				}
				require.NoError(t, mg.AddNode(dep))

				// Add main function
				main := &Node{
					ID:     registry.NewID("", "mainFunc"),
					Kind:   lua.Function,
					Source: "return function() end",
					Method: "main",
				}
				require.NoError(t, mg.AddNode(main))
				require.NoError(t, mg.AddDependency(main.ID, dep.ID, "dep"))

				return main.ID
			},
			options:     NewBuildOptions(),
			expectError: false,
		},
		{
			name: "Invalid entrypoint",
			setup: func(_ *MemoryGraph) registry.ID {
				return registry.NewID("test", "non-existent")
			},
			options:     NewBuildOptions(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := NewMemoryGraph()
			entrypoint := tt.setup(mg)

			compiler := NewCompiler(func(*Node) (*glua.FunctionProto, error) {
				return &glua.FunctionProto{}, nil
			}, 100, 200)

			compiled, err := compiler.Compile(mg, entrypoint, tt.options)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, compiled)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, compiled)
				assert.NotNil(t, compiled.Main)
			}
		})
	}
}

func TestCompiler_PreloadModule(t *testing.T) {
	mg := NewMemoryGraph()

	// Add a module node
	module := &Node{
		ID:     registry.NewID("", "testModulePreload"),
		Kind:   lua.Module,
		Module: &testModule{name: "test"},
	}
	require.NoError(t, mg.AddNode(module))

	compiler := NewCompiler(func(*Node) (*glua.FunctionProto, error) { return &glua.FunctionProto{}, nil }, 100, 200)
	compiled := &CompiledMain{}

	// Test preloading
	preload := Preload{
		Name:     "test",
		ModuleID: module.ID,
	}

	err := compiler.preloadModule(mg, preload, compiled)
	assert.NoError(t, err)
	assert.Len(t, compiled.Preloaded, 1)
	assert.Equal(t, module, compiled.Preloaded[0].Node)
	assert.Equal(t, "test", compiled.Preloaded[0].Name)

	// Test preloading non-existent module
	badPreload := Preload{
		Name:     "bad",
		ModuleID: registry.NewID("test", "non-existent"),
	}
	err = compiler.preloadModule(mg, badPreload, compiled)
	assert.Error(t, err)
}

func TestCompiler_SetProto(t *testing.T) {
	compiler := NewCompiler(func(*Node) (*glua.FunctionProto, error) {
		return nil, assert.AnError
	}, 100, 200)

	testID := registry.NewID("", "bytecodeNode")
	proto := &glua.FunctionProto{
		NumParameters:    2,
		NumUsedRegisters: 5,
	}

	// Set proto directly
	compiler.SetProto(testID, proto)

	// Verify proto is in cache
	cached, ok := compiler.protoCache.Get(testID)
	assert.True(t, ok)
	assert.Equal(t, proto, cached)
	assert.Equal(t, uint8(2), cached.NumParameters)
	assert.Equal(t, uint8(5), cached.NumUsedRegisters)
}

func TestCompiler_SetProto_OverridesCompileFn(t *testing.T) {
	compileCalls := 0
	compiler := NewCompiler(func(*Node) (*glua.FunctionProto, error) {
		compileCalls++
		return &glua.FunctionProto{NumParameters: 99}, nil
	}, 100, 200)

	testID := registry.NewID("", "bytecodeOverride")
	injectedProto := &glua.FunctionProto{
		NumParameters: 3,
	}

	// Set proto before any compilation
	compiler.SetProto(testID, injectedProto)

	// Create a node for getCompiledProto
	node := &Node{
		ID:     testID,
		Kind:   lua.FunctionBytecode,
		Method: "handler",
	}

	// Get compiled proto should return injected one
	result, err := compiler.getCompiledProto(node)
	assert.NoError(t, err)
	assert.Equal(t, uint8(3), result.NumParameters)
	assert.Equal(t, 0, compileCalls, "compile function should not be called when proto is cached")
}
