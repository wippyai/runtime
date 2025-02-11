package __redo

import (
	"errors"
	"github.com/ponyruntime/pony/api/registry"
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

func (m *mockCompileFn) compile(source string) (*glua.FunctionProto, error) {
	m.calls[source]++
	if err, ok := m.errors[source]; ok {
		return nil, err
	}
	return m.results[source], nil
}

// Test basic compilation functionality
func TestCompiler_BasicCompilation(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create a simple test node
	node := &Node{
		ID:     registry.ID{Name: "test"},
		Kind:   "function.lua",
		Source: "function test() return 'hello' end",
		Method: "test",
	}
	mock.results[node.Source] = &glua.FunctionProto{}

	proto, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.NotNil(t, proto)
	assert.Equal(t, 1, mock.calls[node.Source])

	// Test cache hit
	proto2, err := compiler.getCompiledProto(node)
	require.NoError(t, err)
	assert.Equal(t, proto, proto2)
	assert.Equal(t, 1, mock.calls[node.Source], "Should use cached version")
}

// Test compilation error handling
func TestCompiler_CompilationErrors(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Test with syntax error
	errorNode := &Node{
		ID:     registry.ID{Name: "error"},
		Kind:   "function.lua",
		Source: "invalid lua code}",
		Method: "test",
	}
	mock.errors[errorNode.Source] = errors.New("syntax error")

	proto, err := compiler.getCompiledProto(errorNode)
	assert.Error(t, err)
	assert.Nil(t, proto)
}

// Test dependency resolution
func TestCompiler_DependencyResolution(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create a simple dependency chain: main -> dep1 -> dep2
	mainNode := &Node{ID: registry.ID{Name: "main"}, Kind: "function.lua", Source: "function main() return dep1() end", Method: "test"}
	dep1Node := &Node{ID: registry.ID{Name: "dep1"}, Kind: "function.lua", Source: "function dep1() return dep2() end", Method: "test"}
	dep2Node := &Node{ID: registry.ID{Name: "dep2"}, Kind: "function.lua", Source: "function dep2() return 'hello' end", Method: "test"}

	mock.results[mainNode.Source] = &glua.FunctionProto{}
	mock.results[dep1Node.Source] = &glua.FunctionProto{}
	mock.results[dep2Node.Source] = &glua.FunctionProto{}

	// Add nodes to compiler's memory graph
	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(dep1Node))
	require.NoError(t, mg.AddNode(dep2Node))

	// Add dependencies
	require.NoError(t, mg.AddDependency(mainNode.ID, dep1Node.ID, "dep1"))
	require.NoError(t, mg.AddDependency(dep1Node.ID, dep2Node.ID, "dep2"))

	// Compile with dependencies
	compiled, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)
	assert.NotNil(t, compiled.Main)
	assert.Len(t, compiled.Dependencies, 2)
}

// Test module validation
func TestCompiler_ModuleValidation(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create nodes with modules
	mainNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}
	sharedModule := &dummyModule{name: "testModule"}
	moduleNode := &Node{
		ID:     registry.ID{Name: "moduleNode"},
		Kind:   "module.lua",
		Module: sharedModule,
	}

	mock.results[mainNode.Source] = &glua.FunctionProto{}

	// Set up memory graph
	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(moduleNode))
	require.NoError(t, mg.AddDependency(mainNode.ID, moduleNode.ID, "mod"))

	t.Run("AllowAll mode", func(t *testing.T) {
		options := NewBuildOptions().WithAccessMode(AllowAll)
		compiled, err := compiler.Compile(mainNode.ID, options)
		require.NoError(t, err)
		assert.Len(t, compiled.Modules, 1)
	})

	t.Run("DenyAll mode", func(t *testing.T) {
		options := NewBuildOptions().WithAccessMode(DenyAll)
		_, err := compiler.Compile(mainNode.ID, options)
		assert.Error(t, err)
	})
}

// Test circular dependency detection
func TestCompiler_CircularDependency(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create nodes with circular dependency
	nodeA := &Node{ID: registry.ID{Name: "A"}, Kind: "function.lua", Source: "function A() return B() end", Method: "test"}
	nodeB := &Node{ID: registry.ID{Name: "B"}, Kind: "function.lua", Source: "function B() return A() end", Method: "test"}

	mock.results[nodeA.Source] = &glua.FunctionProto{}
	mock.results[nodeB.Source] = &glua.FunctionProto{}

	// Add nodes to graph
	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(nodeA))
	require.NoError(t, mg.AddNode(nodeB))

	// Try to create circular dependency
	require.NoError(t, mg.AddDependency(nodeA.ID, nodeB.ID, "B"))
	err := mg.AddDependency(nodeB.ID, nodeA.ID, "A")
	assert.Error(t, err, "Should detect circular dependency")
}

// Test compilation with multiple dependency paths (diamond pattern)
func TestCompiler_DiamondDependency(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create diamond dependency pattern:
	//      A
	//    /   \
	//   B     C
	//    \   /
	//      D
	nodes := map[string]*Node{
		"A": {ID: registry.ID{Name: "A"}, Kind: "function.lua", Source: "function A() return B() + C() end", Method: "test"},
		"B": {ID: registry.ID{Name: "B"}, Kind: "function.lua", Source: "function B() return D() end", Method: "test"},
		"C": {ID: registry.ID{Name: "C"}, Kind: "function.lua", Source: "function C() return D() end", Method: "test"},
		"D": {ID: registry.ID{Name: "D"}, Kind: "function.lua", Source: "function D() return 1 end", Method: "test"},
	}

	for _, node := range nodes {
		mock.results[node.Source] = &glua.FunctionProto{}
	}

	// Set up the dependency graph
	mg := compiler.memGraph
	for _, node := range nodes {
		require.NoError(t, mg.AddNode(node))
	}

	require.NoError(t, mg.AddDependency(nodes["A"].ID, nodes["B"].ID, "B"))
	require.NoError(t, mg.AddDependency(nodes["A"].ID, nodes["C"].ID, "C"))
	require.NoError(t, mg.AddDependency(nodes["B"].ID, nodes["D"].ID, "D"))
	require.NoError(t, mg.AddDependency(nodes["C"].ID, nodes["D"].ID, "D"))

	// Compile and verify
	compiled, err := compiler.Compile(nodes["A"].ID, nil)
	require.NoError(t, err)
	assert.NotNil(t, compiled.Main)
	assert.Len(t, compiled.Dependencies, 3)
}

// Test module deduplication
func TestCompiler_ModuleDeduplication(t *testing.T) {
	mock := newMockCompileFn()
	compiler := NewCompiler(mock.compile, 10)

	// Create nodes that use the same module
	mainNode := &Node{
		ID:     registry.ID{Name: "main"},
		Kind:   "function.lua",
		Source: "function main() end",
		Method: "test",
	}
	sharedModule := &dummyModule{name: "sharedModule"}
	dep1Node := &Node{
		ID:     registry.ID{Name: "dep1"},
		Kind:   "module.lua",
		Module: sharedModule,
	}
	dep2Node := &Node{
		ID:     registry.ID{Name: "dep2"},
		Kind:   "module.lua",
		Module: sharedModule,
	}

	mock.results[mainNode.Source] = &glua.FunctionProto{}

	// Set up memory graph
	mg := compiler.memGraph
	require.NoError(t, mg.AddNode(mainNode))
	require.NoError(t, mg.AddNode(dep1Node))
	require.NoError(t, mg.AddNode(dep2Node))
	require.NoError(t, mg.AddDependency(mainNode.ID, dep1Node.ID, "dep1"))
	require.NoError(t, mg.AddDependency(mainNode.ID, dep2Node.ID, "dep2"))

	// Compile and verify
	compiled, err := compiler.Compile(mainNode.ID, nil)
	require.NoError(t, err)
	assert.Len(t, compiled.Modules, 1, "Should have only one instance of the shared module")
}
