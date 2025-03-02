package graph

import (
	"reflect"
	"sync"
	"testing"
)

func TestLevels(t *testing.T) {
	t.Run("basic dependency levels", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// Spawn a simple dependency graph
		//     A   B
		//    / \ /
		//   C   D
		//    \ /
		//     E
		nodes := []string{"A", "B", "C", "D", "E"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "dep"}
		g.AddEdge("A", "C", 1, edgeData)
		g.AddEdge("A", "D", 1, edgeData)
		g.AddEdge("B", "D", 1, edgeData)
		g.AddEdge("C", "E", 1, edgeData)
		g.AddEdge("D", "E", 1, edgeData)

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedLevels := [][]string{
			{"A", "B"}, // Level 0: no dependencies
			{"C", "D"}, // Level 1: depends on A/B
			{"E"},      // Level 2: depends on C/D
		}

		if !reflect.DeepEqual(levels.AllLevels(), expectedLevels) {
			t.Errorf("expected levels %v, got %v", expectedLevels, levels.AllLevels())
		}
	})

	t.Run("cycle detection", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Spawn a cycle: A -> B -> C -> A
		edgeData := TestEdgeData{Label: "cycle"}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 1, edgeData)
		g.AddEdge("C", "A", 1, edgeData)

		_, err := g.DependencyLevels()
		if err == nil {
			t.Error("expected cycle detection error")
		}
	})

	t.Run("empty graph", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(levels.levels) != 0 {
			t.Errorf("expected 0 levels for empty graph, got %d", len(levels.levels))
		}
	})

	t.Run("single node", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(levels.levels) != 1 {
			t.Errorf("expected 1 level for single node, got %d", len(levels.levels))
		}
	})

	t.Run("level operations", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddEdge("A", "B", 1, TestEdgeData{Label: "op"})

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Test GetLevel
		level0, err := levels.GetLevel(0)
		if err != nil || !reflect.DeepEqual(level0, []string{"A"}) {
			t.Errorf("GetLevel(0) = %v, want [A]", level0)
		}

		// Test invalid level
		_, err = levels.GetLevel(-1)
		if err == nil {
			t.Error("expected error for invalid level")
		}

		// Test GetNodeLevel
		if level := levels.GetNodeLevel("A"); level != 0 {
			t.Errorf("GetNodeLevel(A) = %d, want 0", level)
		}
		if level := levels.GetNodeLevel("B"); level != 1 {
			t.Errorf("GetNodeLevel(B) = %d, want 1", level)
		}
		if level := levels.GetNodeLevel("X"); level != -1 {
			t.Errorf("GetNodeLevel(X) = %d, want -1", level)
		}
	})

	t.Run("generic type support", func(t *testing.T) {
		// Test with integers
		g := New[int, TestEdgeData]()
		nodes := []int{1, 2, 3}
		for _, node := range nodes {
			g.AddNode(node)
		}
		g.AddEdge(1, 2, 1, TestEdgeData{Label: "int"})
		g.AddEdge(2, 3, 1, TestEdgeData{Label: "int"})

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error with int nodes: %v", err)
		}

		expectedLevels := [][]int{{1}, {2}, {3}}
		if !reflect.DeepEqual(levels.AllLevels(), expectedLevels) {
			t.Errorf("expected levels %v, got %v", expectedLevels, levels.AllLevels())
		}
	})
}

func TestDependencyLevelsComplex(t *testing.T) {
	t.Run("complex branching", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// Spawn a more complex dependency tree:
		//     A   B   C
		//    / \ / \ /
		//   D   E   F
		//    \ /   /
		//     G   /
		//      \ /
		//       H
		nodes := []string{"A", "B", "C", "D", "E", "F", "G", "H"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edges := []struct{ from, to string }{
			{"A", "D"}, {"A", "E"},
			{"B", "E"}, {"B", "F"},
			{"C", "F"},
			{"D", "G"},
			{"E", "G"},
			{"F", "H"},
			{"G", "H"},
		}

		edgeData := TestEdgeData{Label: "complex"}
		for _, e := range edges {
			g.AddEdge(e.from, e.to, 1, edgeData)
		}

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedLevels := [][]string{
			{"A", "B", "C"}, // Level 0
			{"D", "E", "F"}, // Level 1
			{"G"},           // Level 2
			{"H"},           // Level 3
		}

		if !reflect.DeepEqual(levels.AllLevels(), expectedLevels) {
			t.Errorf("got levels %v, want %v", levels.AllLevels(), expectedLevels)
		}
	})

	t.Run("multiple entry and exit points", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// Graph with multiple entry/exit points:
		//   A   B
		//    \ /
		//     C
		//    / \
		//   D   E
		nodes := []string{"A", "B", "C", "D", "E"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "multi"}
		g.AddEdge("A", "C", 1, edgeData)
		g.AddEdge("B", "C", 1, edgeData)
		g.AddEdge("C", "D", 1, edgeData)
		g.AddEdge("C", "E", 1, edgeData)

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if level := levels.GetNodeLevel("C"); level != 1 {
			t.Errorf("node C should be at level 1, got %d", level)
		}

		if level := levels.GetNodeLevel("D"); level != 2 {
			t.Errorf("node D should be at level 2, got %d", level)
		}
	})
}

func TestDependencyLevelsEdgeCases(t *testing.T) {
	t.Run("single chain dependencies", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// A -> B -> C -> D (linear chain)
		nodes := []string{"A", "B", "C", "D"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "chain"}
		for i := 0; i < len(nodes)-1; i++ {
			g.AddEdge(nodes[i], nodes[i+1], 1, edgeData)
		}

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Each node should be in its own level
		for i, node := range nodes {
			if level := levels.GetNodeLevel(node); level != i {
				t.Errorf("node %s should be at level %d, got %d", node, i, level)
			}
		}
	})

	t.Run("skip level dependencies", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// A -> B -> D
		// A ------> D (skip B)
		nodes := []string{"A", "B", "D"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "skip"}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "D", 1, edgeData)
		g.AddEdge("A", "D", 1, edgeData)

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// D should still be at level 2 due to dependency through B
		if level := levels.GetNodeLevel("D"); level != 2 {
			t.Errorf("node D should be at level 2 despite direct edge from A, got %d", level)
		}
	})
}

func TestDependencyLevelsConcurrent(t *testing.T) {
	g := New[string, TestEdgeData]()

	// Setup base graph
	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	edgeData := TestEdgeData{Label: "concurrent"}
	g.AddEdge("A", "B", 1, edgeData)
	g.AddEdge("B", "C", 1, edgeData)
	g.AddEdge("C", "D", 1, edgeData)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			levels, err := g.DependencyLevels()
			if err != nil {
				t.Errorf("concurrent DependencyLevels failed: %v", err)
				return
			}
			// Verify basic properties remain consistent
			if len(levels.levels) != 4 {
				t.Errorf("expected 4 levels in concurrent test, got %d", len(levels.levels))
			}
		}()
	}
	wg.Wait()
}

func TestDependencyLevelsMethodsExtended(t *testing.T) {
	g := New[string, TestEdgeData]()
	nodes := []string{"A", "B", "C"}
	for _, node := range nodes {
		g.AddNode(node)
	}
	g.AddEdge("A", "B", 1, TestEdgeData{Label: "ext"})
	g.AddEdge("B", "C", 1, TestEdgeData{Label: "ext"})

	levels, err := g.DependencyLevels()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("invalid level access", func(t *testing.T) {
		_, err := levels.GetLevel(-1)
		if err == nil {
			t.Error("expected error for negative level")
		}

		_, err = levels.GetLevel(len(levels.levels))
		if err == nil {
			t.Error("expected error for out of bounds level")
		}
	})

	t.Run("level count consistency", func(t *testing.T) {
		allLevels := levels.AllLevels()
		if len(allLevels) != len(levels.levels) {
			t.Errorf("inconsistent level count: AllLevels() len %d != levels len %d",
				len(allLevels), len(levels.levels))
		}
	})

	t.Run("non-existent node level", func(t *testing.T) {
		if level := levels.GetNodeLevel("NonExistent"); level != -1 {
			t.Errorf("expected level -1 for non-existent node, got %d", level)
		}
	})
}

func TestNodeLevelInfinity(t *testing.T) {
	t.Run("get level of non-existent node", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddEdge("A", "B", 1, TestEdgeData{Label: "inf"})

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Test non-existent node returns Infinity
		if level := levels.GetNodeLevel("X"); level != Infinity {
			t.Errorf("expected Infinity for non-existent node, got %d", level)
		}

		// Done node and get new levels
		err = g.RemoveNode("B")
		if err != nil {
			t.Fatalf("unexpected error removing node: %v", err)
		}

		// Need to get new levels after modification
		levels, err = g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error getting new levels: %v", err)
		}

		// B should now return Infinity in new levels as it's no longer in the graph
		if level := levels.GetNodeLevel("B"); level != Infinity {
			t.Errorf("expected Infinity for removed node, got %d", level)
		}
	})

	t.Run("level validation", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddEdge("A", "B", 1, TestEdgeData{Label: "valid"})

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Test that valid nodes don't return Infinity
		if level := levels.GetNodeLevel("A"); level == Infinity {
			t.Error("unexpected Infinity for existing node A")
		}
		if level := levels.GetNodeLevel("B"); level == Infinity {
			t.Error("unexpected Infinity for existing node B")
		}
	})
}
