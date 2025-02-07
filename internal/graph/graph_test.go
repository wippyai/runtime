package graph

import (
	"sort"
	"sync"
	"testing"
)

func TestGraphOperations(t *testing.T) {
	t.Run("edge weights", func(t *testing.T) {
		g := New[string]()
		g.AddNode("A")
		g.AddNode("B")

		// Test adding edge with weight
		g.AddEdge("A", "B", 5)
		if !g.HasEdge("A", "B") {
			t.Error("edge A->B should exist")
		}

		// Test updating edge weight
		g.AddEdge("A", "B", 10)
		if !g.HasEdge("A", "B") {
			t.Error("edge A->B should still exist after weight update")
		}
	})

	t.Run("remove edges", func(t *testing.T) {
		g := New[string]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		g.AddEdge("A", "B", 1)
		g.AddEdge("B", "C", 2)
		g.AddEdge("A", "C", 3)

		// Remove middle node
		err := g.RemoveNode("B")
		if err != nil {
			t.Errorf("unexpected error removing node: %v", err)
		}

		// Verify A->C edge remains
		if !g.HasEdge("A", "C") {
			t.Error("edge A->C should still exist after removing B")
		}
	})

	t.Run("duplicate operations", func(t *testing.T) {
		g := New[string]()

		// Test duplicate node additions
		g.AddNode("A")
		g.AddNode("A") // Should be idempotent

		neighbors, err := g.GetNeighbors("A")
		if err != nil {
			t.Errorf("unexpected error getting neighbors: %v", err)
		}
		if len(neighbors) != 0 {
			t.Errorf("new node should have no neighbors, got %v", neighbors)
		}
	})

	t.Run("edge cases", func(t *testing.T) {
		g := New[string]()

		// Test operations on empty graph
		if g.HasNode("A") {
			t.Error("empty graph should not have nodes")
		}

		if g.HasEdge("A", "B") {
			t.Error("empty graph should not have edges")
		}

		// Test non-existent node operations
		_, err := g.GetNeighbors("A")
		if err == nil {
			t.Error("expected error getting neighbors of non-existent node")
		}
	})

	t.Run("neighbor operations", func(t *testing.T) {
		g := New[string]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		g.AddEdge("A", "B", 1)
		g.AddEdge("A", "C", 2)

		neighbors, err := g.GetNeighbors("A")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(neighbors) != 2 {
			t.Errorf("expected 2 neighbors, got %d", len(neighbors))
		}

		// Remove edge by removing node
		err = g.RemoveNode("B")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		neighbors, err = g.GetNeighbors("A")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		if len(neighbors) != 1 {
			t.Errorf("expected 1 neighbor after removal, got %d", len(neighbors))
		}
	})
}

func TestGraphGenericTypes(t *testing.T) {
	t.Run("integer nodes", func(t *testing.T) {
		g := New[int]()
		g.AddNode(1)
		g.AddNode(2)
		g.AddEdge(1, 2, 5)

		if !g.HasNode(1) || !g.HasNode(2) {
			t.Error("integer nodes should exist")
		}

		if !g.HasEdge(1, 2) {
			t.Error("edge between integer nodes should exist")
		}
	})

	t.Run("custom comparable type", func(t *testing.T) {
		type CustomID string
		g := New[CustomID]()

		g.AddNode("A")
		g.AddNode("B")
		g.AddEdge("A", "B", 1)

		if !g.HasNode("A") || !g.HasNode("B") {
			t.Error("custom type nodes should exist")
		}

		if !g.HasEdge("A", "B") {
			t.Error("edge between custom type nodes should exist")
		}
	})
}

func TestGraphEdgeOperations(t *testing.T) {
	t.Run("edge management", func(t *testing.T) {
		g := New[string]()

		// Add nodes
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Test bidirectional edges
		g.AddEdge("A", "B", 1)
		g.AddEdge("B", "A", 2)

		if !g.HasEdge("A", "B") || !g.HasEdge("B", "A") {
			t.Error("bidirectional edges should exist")
		}

		// Test edge overwrite
		g.AddEdge("A", "B", 3)
		// Would need to add a method to get edge weight to verify the new weight
	})

	t.Run("sorted nodes", func(t *testing.T) {
		g := New[string]()

		// Add nodes in random order
		nodes := []string{"C", "A", "B"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		gotNodes := g.GetNodes()
		sort.Strings(gotNodes)

		// Expected nodes should be sorted
		expectedNodes := []string{"A", "B", "C"}
		for i, node := range gotNodes {
			if node != expectedNodes[i] {
				t.Errorf("sorted nodes mismatch at position %d: got %s, want %s",
					i, node, expectedNodes[i])
			}
		}
	})
}

func TestGraphConcurrentOperations(t *testing.T) {
	t.Run("concurrent node operations", func(t *testing.T) {
		g := New[string]()
		var wg sync.WaitGroup

		// Concurrent node additions
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(val int) {
				defer wg.Done()
				node := string(rune('A' + (val % 26)))
				g.AddNode(node)
			}(i)
		}
		wg.Wait()

		// Verify nodes
		nodes := g.GetNodes()
		if len(nodes) > 26 {
			t.Errorf("expected at most 26 nodes, got %d", len(nodes))
		}
	})

	t.Run("concurrent edge operations", func(t *testing.T) {
		g := New[string]()

		// Add base nodes
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		var wg sync.WaitGroup
		// Concurrent edge additions
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				g.AddEdge("A", "B", 1)
				g.AddEdge("B", "C", 2)
			}()
		}
		wg.Wait()

		// Verify edges exist after concurrent operations
		if !g.HasEdge("A", "B") || !g.HasEdge("B", "C") {
			t.Error("edges should exist after concurrent operations")
		}
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		g := New[string]()
		var wg sync.WaitGroup

		// Setup initial nodes
		initialNodes := []string{"A", "B", "C"}
		for _, node := range initialNodes {
			g.AddNode(node)
		}

		// Concurrent mixed operations
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				g.AddNode("D")
				g.AddEdge("A", "B", 1)
				neighbors, _ := g.GetNeighbors("A")
				_ = neighbors // Use neighbors to prevent compiler optimization
			}()
		}
		wg.Wait()

		// Verify graph integrity
		if !g.HasNode("D") {
			t.Error("node D should exist after concurrent operations")
		}
		if !g.HasEdge("A", "B") {
			t.Error("edge A->B should exist after concurrent operations")
		}
	})
}

func TestGraphNeighborOperations(t *testing.T) {
	t.Run("neighbor order", func(t *testing.T) {
		g := New[string]()

		// Add nodes and edges
		nodes := []string{"A", "B", "C", "D"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Add edges in non-alphabetical order
		g.AddEdge("A", "C", 1)
		g.AddEdge("A", "B", 2)
		g.AddEdge("A", "D", 3)

		// Get neighbors and verify they can be sorted
		neighbors, err := g.GetNeighbors("A")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Sort neighbors
		sort.Strings(neighbors)
		expected := []string{"B", "C", "D"}

		// Verify order
		for i, node := range neighbors {
			if node != expected[i] {
				t.Errorf("sorted neighbors mismatch at position %d: got %s, want %s",
					i, node, expected[i])
			}
		}
	})
}

func TestGraphEdgeCleanup(t *testing.T) {
	t.Run("bidirectional edge removal", func(t *testing.T) {
		g := New[string]()
		g.AddNode("A")
		g.AddNode("B")

		// Add edges in both directions
		g.AddEdge("A", "B", 1)
		g.AddEdge("B", "A", 2)

		// Remove edges between A and B
		err := g.RemoveNode("B")
		if err != nil {
			t.Errorf("unexpected error removing node: %v", err)
		}

		// Verify A's edge map exists but has no edges
		neighbors, err := g.GetNeighbors("A")
		if err != nil {
			t.Errorf("unexpected error getting neighbors: %v", err)
		}
		if len(neighbors) != 0 {
			t.Error("A should have no neighbors after B removal")
		}
	})

	t.Run("edge map cleanup after multiple operations", func(t *testing.T) {
		g := New[string]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		// Create and remove edges in various patterns
		g.AddEdge("A", "B", 1)
		g.AddEdge("B", "C", 2)
		g.AddEdge("C", "A", 3)

		// Remove middle node
		err := g.RemoveNode("B")
		if err != nil {
			t.Errorf("unexpected error removing node: %v", err)
		}

		// Check remaining edges
		neighborsA, _ := g.GetNeighbors("A")

		if len(neighborsA) != 0 {
			t.Error("A should have no outgoing edges after B removal")
		}

		// Add new edges after removal
		g.AddEdge("A", "C", 4)
		neighborsA, _ = g.GetNeighbors("A")
		if len(neighborsA) != 1 {
			t.Error("A should have exactly one neighbor after adding new edge")
		}
	})

	t.Run("edge map complete cleanup", func(t *testing.T) {
		g := New[string]()

		// Setup complete graph
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Create edges between all nodes
		for i := 0; i < len(nodes); i++ {
			for j := 0; j < len(nodes); j++ {
				if i != j {
					g.AddEdge(nodes[i], nodes[j], 1)
				}
			}
		}

		// Remove all nodes one by one
		for _, node := range nodes {
			err := g.RemoveNode(node)
			if err != nil {
				t.Errorf("unexpected error removing node %s: %v", node, err)
			}
		}

		// Verify graph is empty
		remainingNodes := g.GetNodes()
		if len(remainingNodes) != 0 {
			t.Error("graph should have no nodes after removing all")
		}

		// Add new node and verify clean state
		g.AddNode("D")
		neighbors, err := g.GetNeighbors("D")
		if err != nil {
			t.Errorf("unexpected error getting neighbors: %v", err)
		}
		if len(neighbors) != 0 {
			t.Error("new node should have empty edge map")
		}
	})
}
