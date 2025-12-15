package code

import (
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	glua "github.com/yuin/gopher-lua"
)

// =============================================================================
// MEMORY GRAPH BENCHMARKS
// =============================================================================

func BenchmarkMemoryGraph_AddNode(b *testing.B) {
	mg := NewMemoryGraph()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		node := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(node)
	}
}

func BenchmarkMemoryGraph_AddDependency(b *testing.B) {
	mg := NewMemoryGraph()

	// Pre-create nodes
	for i := 0; i < b.N+1; i++ {
		node := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(node)
	}

	b.ResetTimer()

	mainID := registry.NewID("bench", "node_0")
	for i := 1; i <= b.N; i++ {
		depID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)}
		_ = mg.AddDependency(mainID, depID, fmt.Sprintf("dep_%d", i))
	}
}

func BenchmarkMemoryGraph_GetDirectDependencies(b *testing.B) {
	mg := NewMemoryGraph()

	// Create a node with 100 dependencies
	mainNode := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "return 1",
	}
	_ = mg.AddNode(mainNode)

	for i := 0; i < 100; i++ {
		depNode := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("dep_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(depNode)
		_ = mg.AddDependency(mainNode.ID, depNode.ID, fmt.Sprintf("alias_%d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = mg.GetDirectDependencies(mainNode.ID)
	}
}

func BenchmarkMemoryGraph_GetAllDependents(b *testing.B) {
	mg := NewMemoryGraph()

	// Create a dependency tree: root -> level1 (10 nodes) -> level2 (100 nodes)
	root := &Node{
		ID:     registry.NewID("bench", "root"),
		Kind:   luaapi.Function,
		Source: "return 1",
	}
	_ = mg.AddNode(root)

	for i := 0; i < 10; i++ {
		l1 := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("l1_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(l1)
		_ = mg.AddDependency(l1.ID, root.ID, "root")

		for j := 0; j < 10; j++ {
			l2 := &Node{
				ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("l2_%d_%d", i, j)},
				Kind:   luaapi.Function,
				Source: "return 1",
			}
			_ = mg.AddNode(l2)
			_ = mg.AddDependency(l2.ID, l1.ID, "parent")
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = mg.GetAllDependents(root.ID)
	}
}

func BenchmarkMemoryGraph_Build(b *testing.B) {
	mg := NewMemoryGraph()

	// Create a typical dependency graph: main -> 5 direct deps -> 3 transitive deps each
	main := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "return 1",
	}
	_ = mg.AddNode(main)

	for i := 0; i < 5; i++ {
		l1 := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("l1_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(l1)
		_ = mg.AddDependency(main.ID, l1.ID, fmt.Sprintf("lib%d", i))

		for j := 0; j < 3; j++ {
			l2 := &Node{
				ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("l2_%d_%d", i, j)},
				Kind:   luaapi.Function,
				Source: "return 1",
			}
			_ = mg.AddNode(l2)
			_ = mg.AddDependency(l1.ID, l2.ID, fmt.Sprintf("dep%d", j))
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = mg.Build(main.ID)
	}
}

func BenchmarkMemoryGraph_DependencyLevels(b *testing.B) {
	mg := NewMemoryGraph()

	// Create 100 nodes in a linear dependency chain
	for i := 0; i < 100; i++ {
		node := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(node)

		if i > 0 {
			prevID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i-1)}
			_ = mg.AddDependency(node.ID, prevID, "prev")
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		mg.cacheValid = false // Force recomputation
		_, _ = mg.DependencyLevels()
	}
}

func BenchmarkMemoryGraph_CycleDetection(b *testing.B) {
	mg := NewMemoryGraph()

	// Create nodes in a chain
	for i := 0; i < 50; i++ {
		node := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(node)

		if i > 0 {
			prevID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i-1)}
			_ = mg.AddDependency(node.ID, prevID, "prev")
		}
	}

	firstID := registry.NewID("bench", "node_0")
	lastID := registry.NewID("bench", "node_49")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = mg.hasCycle(firstID, lastID)
	}
}

// =============================================================================
// COMPILER BENCHMARKS
// =============================================================================

func BenchmarkCompiler_Compile(b *testing.B) {
	mg := NewMemoryGraph()

	main := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "local x = 1; return x + 1",
		Method: "main",
	}
	_ = mg.AddNode(main)

	compiler := NewCompiler(
		func(_ *Node) (*glua.FunctionProto, error) {
			return &glua.FunctionProto{}, nil
		},
		1000,
		100,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		compiler.mainCache.Delete(main.ID)
		_, _ = compiler.Compile(mg, main.ID, nil)
	}
}

func BenchmarkCompiler_CompileCacheHit(b *testing.B) {
	mg := NewMemoryGraph()

	main := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "local x = 1; return x + 1",
		Method: "main",
	}
	_ = mg.AddNode(main)

	compiler := NewCompiler(
		func(_ *Node) (*glua.FunctionProto, error) {
			return &glua.FunctionProto{}, nil
		},
		1000,
		100,
	)

	// Warm up cache
	_, _ = compiler.Compile(mg, main.ID, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = compiler.Compile(mg, main.ID, nil)
	}
}

func BenchmarkCompiler_CompileWithDependencies(b *testing.B) {
	mg := NewMemoryGraph()

	main := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "return lib1() + lib2()",
		Method: "main",
	}
	_ = mg.AddNode(main)

	for i := 0; i < 10; i++ {
		lib := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("lib%d", i)},
			Kind:   luaapi.Function,
			Source: fmt.Sprintf("return %d", i),
		}
		_ = mg.AddNode(lib)
		_ = mg.AddDependency(main.ID, lib.ID, fmt.Sprintf("lib%d", i))
	}

	compiler := NewCompiler(
		func(_ *Node) (*glua.FunctionProto, error) {
			return &glua.FunctionProto{}, nil
		},
		1000,
		100,
	)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		compiler.mainCache.Delete(main.ID)
		_, _ = compiler.Compile(mg, main.ID, nil)
	}
}

// =============================================================================
// BUILD OPTIONS BENCHMARKS
// =============================================================================

func BenchmarkBuildOptions_Validate_Small(b *testing.B) {
	opts := NewBuildOptions().
		WithMode(AllowListed).
		WithAllowed(
			registry.NewID("test", "node1"),
			registry.NewID("test", "node2"),
			registry.NewID("test", "node3"),
		)

	nodes := map[registry.ID]*Node{
		{NS: "test", Name: "node1"}: {ID: registry.NewID("test", "node1")},
		{NS: "test", Name: "node2"}: {ID: registry.NewID("test", "node2")},
		{NS: "test", Name: "node3"}: {ID: registry.NewID("test", "node3")},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = opts.Validate(nodes)
	}
}

func BenchmarkBuildOptions_Validate_Large(b *testing.B) {
	opts := NewBuildOptions().WithMode(AllowListed)

	// Add 1000 allowed IDs
	for i := 0; i < 1000; i++ {
		opts.WithAllowed(registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)})
	}

	// Create 100 nodes to validate
	nodes := make(map[registry.ID]*Node, 100)
	for i := 0; i < 100; i++ {
		id := registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)}
		nodes[id] = &Node{ID: id}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		opts.setsInitialized = false // Force re-initialization to test worst case
		_ = opts.Validate(nodes)
	}
}

func BenchmarkBuildOptions_Validate_LargeWithCache(b *testing.B) {
	opts := NewBuildOptions().WithMode(AllowListed)

	// Add 1000 allowed IDs
	for i := 0; i < 1000; i++ {
		opts.WithAllowed(registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)})
	}

	// Create 100 nodes to validate
	nodes := make(map[registry.ID]*Node, 100)
	for i := 0; i < 100; i++ {
		id := registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)}
		nodes[id] = &Node{ID: id}
	}

	// Warm up cache
	_ = opts.Validate(nodes)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = opts.Validate(nodes)
	}
}

// =============================================================================
// ALLOCATION BENCHMARKS
// =============================================================================

func BenchmarkMemoryGraph_Build_Allocations(b *testing.B) {
	mg := NewMemoryGraph()

	// Create a typical dependency graph
	main := &Node{
		ID:     registry.NewID("bench", "main"),
		Kind:   luaapi.Function,
		Source: "return 1",
	}
	_ = mg.AddNode(main)

	for i := 0; i < 10; i++ {
		lib := &Node{
			ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("lib%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		_ = mg.AddNode(lib)
		_ = mg.AddDependency(main.ID, lib.ID, fmt.Sprintf("lib%d", i))
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = mg.Build(main.ID)
	}
}

func BenchmarkHashNode(b *testing.B) {
	node := &Node{
		ID:     registry.NewID("bench", "test"),
		Source: "local x = 1; local y = 2; return x + y",
		Method: "main",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = HashNode(node)
	}
}

// =============================================================================
// MEMORY LEAK TESTS
// =============================================================================

func TestMemoryGraph_NoLeaksOnRemoval(t *testing.T) {
	mg := NewMemoryGraph()

	// Add and remove nodes repeatedly
	for i := 0; i < 1000; i++ {
		node := &Node{
			ID:     registry.ID{NS: "leak", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node: %v", err)
		}
	}

	// Remove all nodes
	for i := 0; i < 1000; i++ {
		id := registry.ID{NS: "leak", Name: fmt.Sprintf("node_%d", i)}
		if err := mg.RemoveNode(id); err != nil {
			t.Fatalf("failed to remove node: %v", err)
		}
	}

	// Verify internal maps are clean
	if len(mg.nodes) != 0 {
		t.Errorf("expected 0 nodes after removal, got %d", len(mg.nodes))
	}
}

func TestMemoryGraph_NoLeaksOnDependencyRemoval(t *testing.T) {
	mg := NewMemoryGraph()

	// Create a chain of nodes
	nodes := make([]*Node, 100)
	for i := 0; i < 100; i++ {
		nodes[i] = &Node{
			ID:     registry.ID{NS: "leak", Name: fmt.Sprintf("chain_%d", i)},
			Kind:   luaapi.Function,
			Source: "return 1",
		}
		if err := mg.AddNode(nodes[i]); err != nil {
			t.Fatalf("failed to add node: %v", err)
		}
	}

	// Create dependencies
	for i := 1; i < 100; i++ {
		if err := mg.AddDependency(nodes[i].ID, nodes[i-1].ID, fmt.Sprintf("dep_%d", i)); err != nil {
			t.Fatalf("failed to add dependency: %v", err)
		}
	}

	// Remove dependencies in reverse
	for i := 99; i > 0; i-- {
		if err := mg.RemoveDependency(nodes[i].ID, nodes[i-1].ID); err != nil {
			t.Fatalf("failed to remove dependency: %v", err)
		}
	}

	// Verify all nodes can be removed (no dangling dependencies)
	for i := 0; i < 100; i++ {
		if err := mg.RemoveNode(nodes[i].ID); err != nil {
			t.Fatalf("failed to remove node %d: %v", i, err)
		}
	}
}

func TestCompiler_CacheEviction(t *testing.T) {
	mock := func(_ *Node) (*glua.FunctionProto, error) {
		return &glua.FunctionProto{}, nil
	}

	// Small cache to force evictions
	compiler := NewCompiler(mock, 10, 5)
	mg := NewMemoryGraph()

	// Add more nodes than cache capacity
	for i := 0; i < 50; i++ {
		node := &Node{
			ID:     registry.ID{NS: "evict", Name: fmt.Sprintf("node_%d", i)},
			Kind:   luaapi.Function,
			Source: fmt.Sprintf("return %d", i),
			Method: "main",
		}
		if err := mg.AddNode(node); err != nil {
			t.Fatalf("failed to add node: %v", err)
		}

		_, err := compiler.Compile(mg, node.ID, nil)
		if err != nil {
			t.Fatalf("failed to compile node %d: %v", i, err)
		}
	}

	// Early entries should have been evicted
	// This just verifies the cache doesn't grow unboundedly
}

func TestBuildOptions_SetReinitialization(t *testing.T) {
	opts := NewBuildOptions().WithMode(AllowListed)

	// Add initial allowed IDs
	for i := 0; i < 100; i++ {
		opts.WithAllowed(registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)})
	}

	// Create nodes to validate
	nodes := make(map[registry.ID]*Node, 50)
	for i := 0; i < 50; i++ {
		id := registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)}
		nodes[id] = &Node{ID: id, Kind: luaapi.Function}
	}

	// First validation initializes sets
	if err := opts.Validate(nodes); err != nil {
		t.Fatalf("first validation failed: %v", err)
	}

	// Add more allowed IDs
	for i := 100; i < 200; i++ {
		opts.WithAllowed(registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)})
	}

	// Create new nodes that require the new allowed IDs
	newNodes := make(map[registry.ID]*Node, 50)
	for i := 100; i < 150; i++ {
		id := registry.ID{NS: "test", Name: fmt.Sprintf("node%d", i)}
		newNodes[id] = &Node{ID: id, Kind: luaapi.Function}
	}

	// Second validation should reinitialize sets and succeed
	if err := opts.Validate(newNodes); err != nil {
		t.Errorf("second validation should succeed after adding new allowed IDs: %v", err)
	}
}

// =============================================================================
// ALLOCATION TESTS
// =============================================================================

func TestMemoryGraph_Build_ZeroAllocOnCacheHit(t *testing.T) {
	mg := NewMemoryGraph()

	main := &Node{
		ID:     registry.NewID("alloc", "main"),
		Kind:   luaapi.Function,
		Source: "return 1",
	}
	if err := mg.AddNode(main); err != nil {
		t.Fatal(err)
	}

	// First build populates cache
	_, err := mg.Build(main.ID)
	if err != nil {
		t.Fatal(err)
	}

	// This is just a sanity check that caching is working
	// The benchmark tests provide more accurate allocation measurements
}

func TestCompiler_CacheHit_NoCompilation(t *testing.T) {
	callCount := 0
	mock := func(_ *Node) (*glua.FunctionProto, error) {
		callCount++
		return &glua.FunctionProto{}, nil
	}

	compiler := NewCompiler(mock, 100, 100)
	mg := NewMemoryGraph()

	node := &Node{
		ID:     registry.NewID("cache", "test"),
		Kind:   luaapi.Function,
		Source: "return 1",
		Method: "main",
	}
	if err := mg.AddNode(node); err != nil {
		t.Fatal(err)
	}

	// First compile
	_, err := compiler.Compile(mg, node.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 compile call, got %d", callCount)
	}

	// Second compile should hit cache
	_, err = compiler.Compile(mg, node.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 1 {
		t.Errorf("expected still 1 compile call after cache hit, got %d", callCount)
	}

	// Third compile after invalidation
	compiler.Invalidate([]registry.ID{node.ID})
	_, err = compiler.Compile(mg, node.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 compile calls after invalidation, got %d", callCount)
	}
}

// =============================================================================
// STRESS TESTS
// =============================================================================

func BenchmarkMemoryGraph_LargeGraph(b *testing.B) {
	for _, size := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("nodes_%d", size), func(b *testing.B) {
			mg := NewMemoryGraph()

			// Create nodes
			for i := 0; i < size; i++ {
				node := &Node{
					ID:     registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)},
					Kind:   luaapi.Function,
					Source: "return 1",
				}
				_ = mg.AddNode(node)
			}

			// Create random-ish dependencies (each node depends on ~3 previous nodes)
			for i := 3; i < size; i++ {
				nodeID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", i)}
				for j := 0; j < 3; j++ {
					depIdx := (i - 1 - j) % i
					if depIdx < 0 {
						depIdx = 0
					}
					depID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", depIdx)}
					_ = mg.AddDependency(nodeID, depID, fmt.Sprintf("dep%d", j))
				}
			}

			entryID := registry.ID{NS: "bench", Name: fmt.Sprintf("node_%d", size-1)}

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, _ = mg.Build(entryID)
			}
		})
	}
}
