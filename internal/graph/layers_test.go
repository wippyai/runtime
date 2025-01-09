package graph

import (
	"reflect"
	"sync"
	"testing"
)

func TestDependencyLevels(t *testing.T) {
	t.Run("Basic dependency levels", func(t *testing.T) {
		g := NewGraph()

		// Add nodes
		nodes := []Node{"A", "B", "C", "D", "E", "F"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Add edges representing dependencies
		g.AddEdge(Edge{From: "A", To: "C", Weight: 1})
		g.AddEdge(Edge{From: "A", To: "D", Weight: 1})
		g.AddEdge(Edge{From: "B", To: "D", Weight: 1})
		g.AddEdge(Edge{From: "C", To: "E", Weight: 1})
		g.AddEdge(Edge{From: "D", To: "E", Weight: 1})
		g.AddEdge(Edge{From: "D", To: "F", Weight: 1})
		g.AddEdge(Edge{From: "E", To: "F", Weight: 1})

		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels: %v", err)
		}

		// Expected levels:
		// Level 0: [A B]   (nodes with no dependencies)
		// Level 1: [C D]   (nodes depending on A/B)
		// Level 2: [E]     (nodes depending on C/D)
		// Level 3: [F]     (nodes depending on D/E)

		expectedLevels := [][]Node{
			{"A", "B"},
			{"C", "D"},
			{"E"},
			{"F"},
		}

		if len(deps.Levels) != len(expectedLevels) {
			t.Errorf("Expected %d levels, got %d", len(expectedLevels), len(deps.Levels))
		}

		// Test each level
		for i, expectedLevel := range expectedLevels {
			actualLevel, err := deps.GetLevel(i)
			if err != nil {
				t.Errorf("RaiseError getting level %d: %v", i, err)
				continue
			}

			// Convert slices to sets for comparison
			expectedSet := make(map[Node]bool)
			actualSet := make(map[Node]bool)

			for _, node := range expectedLevel {
				expectedSet[node] = true
			}
			for _, node := range actualLevel {
				actualSet[node] = true
			}

			// Compare sets
			if !reflect.DeepEqual(expectedSet, actualSet) {
				t.Errorf("Level %d mismatch: expected %v, got %v", i, expectedLevel, actualLevel)
			}
		}
	})

	t.Run("Empty graph", func(t *testing.T) {
		g := NewGraph()
		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels for empty graph: %v", err)
		}
		if len(deps.Levels) != 0 {
			t.Errorf("Expected 0 levels for empty graph, got %d", len(deps.Levels))
		}
	})

	t.Run("Single node", func(t *testing.T) {
		g := NewGraph()
		g.AddNode("A")
		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels for single node: %v", err)
		}
		if len(deps.Levels) != 1 {
			t.Errorf("Expected 1 level for single node, got %d", len(deps.Levels))
		}
	})

	t.Run("Linear chain", func(t *testing.T) {
		g := NewGraph()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")
		g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
		g.AddEdge(Edge{From: "B", To: "C", Weight: 1})

		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels for linear chain: %v", err)
		}

		expectedLevels := [][]Node{{"A"}, {"B"}, {"C"}}
		if !reflect.DeepEqual(deps.Levels, expectedLevels) {
			t.Errorf("Linear chain levels mismatch: expected %v, got %v", expectedLevels, deps.Levels)
		}
	})

	t.Run("Diamond shape", func(t *testing.T) {
		g := NewGraph()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")
		g.AddNode("D")
		g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
		g.AddEdge(Edge{From: "A", To: "C", Weight: 1})
		g.AddEdge(Edge{From: "B", To: "D", Weight: 1})
		g.AddEdge(Edge{From: "C", To: "D", Weight: 1})

		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels for diamond shape: %v", err)
		}

		expectedLevels := [][]Node{{"A"}, {"B", "C"}, {"D"}}
		if !reflect.DeepEqual(deps.Levels, expectedLevels) {
			t.Errorf("Diamond shape levels mismatch: expected %v, got %v", expectedLevels, deps.Levels)
		}
	})

	t.Run("Multiple components", func(t *testing.T) {
		g := NewGraph()
		// Component 1
		g.AddNode("A")
		g.AddNode("B")
		g.AddEdge(Edge{From: "A", To: "B", Weight: 1})

		// Component 2
		g.AddNode("C")
		g.AddNode("D")
		g.AddEdge(Edge{From: "C", To: "D", Weight: 1})

		deps, err := g.DependencyLevels()
		if err != nil {
			t.Fatalf("Failed to create dependency levels for multiple components: %v", err)
		}

		if len(deps.Levels) != 2 {
			t.Errorf("Expected 2 levels for multiple components, got %d", len(deps.Levels))
		}
	})
}

func TestDependencyLevels_Cycle(t *testing.T) {
	t.Run("Simple cycle", func(t *testing.T) {
		g := NewGraph()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
		g.AddEdge(Edge{From: "B", To: "C", Weight: 1})
		g.AddEdge(Edge{From: "C", To: "A", Weight: 1}) // Creates a cycle

		_, err := g.DependencyLevels()
		if err == nil {
			t.Error("Expected error for graph with cycle, got nil")
		}
	})

	t.Run("Self cycle", func(t *testing.T) {
		g := NewGraph()
		g.AddNode("A")
		g.AddEdge(Edge{From: "A", To: "A", Weight: 1}) // Self-cycle

		_, err := g.DependencyLevels()
		if err == nil {
			t.Error("Expected error for graph with self-cycle, got nil")
		}
	})
}

func TestDependencyLevels_Methods(t *testing.T) {
	g := NewGraph()
	nodes := []Node{"A", "B", "C"}
	for _, node := range nodes {
		g.AddNode(node)
	}
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "B", To: "C", Weight: 1})

	deps, err := g.DependencyLevels()
	if err != nil {
		t.Fatalf("Failed to create dependency levels: %v", err)
	}

	t.Run("GetLevel bounds checking", func(t *testing.T) {
		// Test negative index
		if _, err := deps.GetLevel(Infinity); err == nil {
			t.Error("Expected error for negative level index")
		}

		// Test out of bounds index
		if _, err := deps.GetLevel(len(deps.Levels)); err == nil {
			t.Error("Expected error for out of bounds level index")
		}
	})

	t.Run("GetNodeLevel", func(t *testing.T) {
		testCases := []struct {
			node     Node
			expected int
		}{
			{"A", 0},
			{"B", 1},
			{"C", 2},
			{"X", Infinity}, // Non-existent node
		}

		for _, tc := range testCases {
			actual := deps.GetNodeLevel(tc.node)
			if actual != tc.expected {
				t.Errorf("GetNodeLevel(%s) = %d, expected %d", tc.node, actual, tc.expected)
			}
		}
	})

	t.Run("LevelCount", func(t *testing.T) {
		expected := 3 // A->B->C should have 3 levels
		if count := deps.LevelCount(); count != expected {
			t.Errorf("LevelCount() = %d, expected %d", count, expected)
		}
	})
}

func TestDependencyLevels_Concurrent(t *testing.T) {
	g := NewGraph()
	nodes := []Node{"A", "B", "C", "D"}
	for _, node := range nodes {
		g.AddNode(node)
	}
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "B", To: "C", Weight: 1})
	g.AddEdge(Edge{From: "C", To: "D", Weight: 1})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deps, err := g.DependencyLevels()
			if err != nil {
				t.Errorf("Concurrent NewDependencyLevels failed: %v", err)
				return
			}
			if deps.LevelCount() != 4 {
				t.Errorf("Expected 4 levels in concurrent test, got %d", deps.LevelCount())
			}
		}()
	}
	wg.Wait()
}
