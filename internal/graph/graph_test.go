package graph

import (
	"sync"
	"testing"
)

type TestEdgeData struct {
	Label string
	Cost  float64
}

func TestGraphOperations(t *testing.T) {
	t.Run("edge weights and data", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		edgeData := TestEdgeData{Label: "test", Cost: 5.5}
		g.AddEdge("A", "B", 5, edgeData)

		if !g.HasEdge("A", "B") {
			t.Error("edge A->B should exist")
		}

		edge, exists := g.GetEdge("A", "B")
		if !exists {
			t.Error("edge A->B should be retrievable")
		}
		if edge.Weight != 5 {
			t.Errorf("expected weight 5, got %d", edge.Weight)
		}
		if edge.Data.Label != "test" {
			t.Errorf("expected label 'test', got %s", edge.Data.Label)
		}

		// Test updating edge
		newData := TestEdgeData{Label: "updated", Cost: 10.5}
		g.AddEdge("A", "B", 10, newData)

		edge, exists = g.GetEdge("A", "B")
		if !exists {
			t.Error("edge A->B should exist after update")
		}
		if edge.Weight != 10 {
			t.Errorf("expected updated weight 10, got %d", edge.Weight)
		}
		if edge.Data.Label != "updated" {
			t.Errorf("expected updated label 'updated', got %s", edge.Data.Label)
		}
	})

	t.Run("remove edges", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 2, edgeData)
		g.AddEdge("A", "C", 3, edgeData)

		err := g.RemoveNode("B")
		if err != nil {
			t.Errorf("unexpected error removing node: %v", err)
		}

		if !g.HasEdge("A", "C") {
			t.Error("edge A->C should still exist after removing B")
		}

		edge, exists := g.GetEdge("A", "C")
		if !exists {
			t.Error("edge A->C should be retrievable after removing B")
		}
		if edge.Weight != 3 {
			t.Errorf("expected weight 3, got %d", edge.Weight)
		}
	})

	t.Run("duplicate operations", func(t *testing.T) {
		g := New[string, TestEdgeData]()

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
		g := New[string, TestEdgeData]()

		if g.HasNode("A") {
			t.Error("empty graph should not have nodes")
		}

		if g.HasEdge("A", "B") {
			t.Error("empty graph should not have edges")
		}

		_, err := g.GetNeighbors("A")
		if err == nil {
			t.Error("expected error getting neighbors of non-existent node")
		}

		_, exists := g.GetEdge("A", "B")
		if exists {
			t.Error("should not find edge in empty graph")
		}
	})
}

func TestGraphGenericTypes(t *testing.T) {
	t.Run("integer nodes", func(t *testing.T) {
		type IntEdgeData struct {
			Value int
		}
		g := New[int, IntEdgeData]()
		g.AddNode(1)
		g.AddNode(2)

		edgeData := IntEdgeData{Value: 42}
		g.AddEdge(1, 2, 5, edgeData)

		if !g.HasNode(1) || !g.HasNode(2) {
			t.Error("integer nodes should exist")
		}

		edge, exists := g.GetEdge(1, 2)
		if !exists {
			t.Error("edge between integer nodes should exist")
		}
		if edge.Data.Value != 42 {
			t.Errorf("expected edge data value 42, got %d", edge.Data.Value)
		}
	})

	t.Run("custom comparable type", func(t *testing.T) {
		type CustomID string
		type CustomEdgeData struct {
			Info string
		}
		g := New[CustomID, CustomEdgeData]()

		g.AddNode("A")
		g.AddNode("B")

		edgeData := CustomEdgeData{Info: "custom"}
		g.AddEdge("A", "B", 1, edgeData)

		if !g.HasNode("A") || !g.HasNode("B") {
			t.Error("custom type nodes should exist")
		}

		edge, exists := g.GetEdge("A", "B")
		if !exists {
			t.Error("edge between custom type nodes should exist")
		}
		if edge.Data.Info != "custom" {
			t.Errorf("expected edge data info 'custom', got %s", edge.Data.Info)
		}
	})
}

func TestGraphConcurrentOperations(t *testing.T) {
	t.Run("concurrent edge operations", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				edgeData := TestEdgeData{
					Label: string(rune('a' + i%26)),
					Cost:  float64(i),
				}
				g.AddEdge("A", "B", 1, edgeData)
				g.AddEdge("B", "C", 2, edgeData)
			}(i)
		}
		wg.Wait()

		if !g.HasEdge("A", "B") || !g.HasEdge("B", "C") {
			t.Error("edges should exist after concurrent operations")
		}
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		var wg sync.WaitGroup

		initialNodes := []string{"A", "B", "C"}
		for _, node := range initialNodes {
			g.AddNode(node)
		}

		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				g.AddNode("D")
				edgeData := TestEdgeData{
					Label: string(rune('a' + i%26)),
					Cost:  float64(i),
				}
				g.AddEdge("A", "B", 1, edgeData)
				neighbors, _ := g.GetNeighbors("A")
				_ = neighbors
			}(i)
		}
		wg.Wait()

		if !g.HasNode("D") {
			t.Error("node D should exist after concurrent operations")
		}
		if !g.HasEdge("A", "B") {
			t.Error("edge A->B should exist after concurrent operations")
		}
	})
}

func TestGraphPathOperations(t *testing.T) {
	t.Run("shortest path with custom edges", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// Spawn a simple path A -> B -> C
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "path"}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 2, edgeData)
		g.AddEdge("A", "C", 4, edgeData) // Longer direct path

		path, err := g.ShortestPath("A", "C")
		if err != nil {
			t.Errorf("unexpected error finding path: %v", err)
		}

		expectedPath := []string{"A", "B", "C"}
		if len(path.Nodes) != len(expectedPath) {
			t.Errorf("expected path length %d, got %d", len(expectedPath), len(path.Nodes))
		}
		for i, node := range path.Nodes {
			if node != expectedPath[i] {
				t.Errorf("path mismatch at position %d: expected %s, got %s",
					i, expectedPath[i], node)
			}
		}
		if path.Cost != 3 {
			t.Errorf("expected total cost 3, got %d", path.Cost)
		}
	})
}

func TestGraphEdgeDataOperations(t *testing.T) {
	t.Run("edge data persistence", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		edgeData := TestEdgeData{
			Label: "original",
			Cost:  1.5,
		}
		g.AddEdge("A", "B", 1, edgeData)

		edge, exists := g.GetEdge("A", "B")
		if !exists {
			t.Error("edge should exist")
		}
		if edge.Data.Label != "original" {
			t.Errorf("expected label 'original', got %s", edge.Data.Label)
		}
		if edge.Data.Cost != 1.5 {
			t.Errorf("expected cost 1.5, got %f", edge.Data.Cost)
		}

		// Update edge data
		newData := TestEdgeData{
			Label: "updated",
			Cost:  2.5,
		}
		g.AddEdge("A", "B", 1, newData)

		edge, exists = g.GetEdge("A", "B")
		if !exists {
			t.Error("edge should exist after update")
		}
		if edge.Data.Label != "updated" {
			t.Errorf("expected updated label 'updated', got %s", edge.Data.Label)
		}
		if edge.Data.Cost != 2.5 {
			t.Errorf("expected updated cost 2.5, got %f", edge.Data.Cost)
		}
	})

	t.Run("nil edge data", func(t *testing.T) {
		g := New[string, *TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		// AddCleanup edge with nil data
		g.AddEdge("A", "B", 1, nil)

		edge, exists := g.GetEdge("A", "B")
		if !exists {
			t.Error("edge should exist")
		}
		if edge.Data != nil {
			t.Error("edge data should be nil")
		}
	})
}

func TestGraphDependencyLevels(t *testing.T) {
	t.Run("dependency levels with custom edges", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		// AddCleanup nodes
		nodes := []string{"A", "B", "C", "D"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		// Spawn dependencies
		edgeData := TestEdgeData{Label: "dep"}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 1, edgeData)
		g.AddEdge("A", "C", 1, edgeData)
		g.AddEdge("C", "D", 1, edgeData)

		levels, err := g.DependencyLevels()
		if err != nil {
			t.Errorf("unexpected error getting dependency levels: %v", err)
		}

		// Verify first level contains only A
		if len(levels.levels[0]) != 1 || levels.levels[0][0] != "A" {
			t.Error("first level should contain only A")
		}

		// Verify D is in the last level
		lastLevel := levels.levels[len(levels.levels)-1]
		if len(lastLevel) != 1 || lastLevel[0] != "D" {
			t.Error("last level should contain only D")
		}
	})

	t.Run("cyclic dependencies", func(t *testing.T) {
		g := New[string, TestEdgeData]()

		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			g.AddNode(node)
		}

		edgeData := TestEdgeData{Label: "cycle"}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 1, edgeData)
		g.AddEdge("C", "A", 1, edgeData)

		_, err := g.DependencyLevels()
		if err == nil {
			t.Error("expected error for cyclic dependencies")
		}
	})
}

func TestGraphMissingDependencyNotAutoAdded(t *testing.T) {
	g := New[string, TestEdgeData]()

	// Only add the source node.
	g.AddNode("A")

	edgeData := TestEdgeData{Label: "dependency", Cost: 1.0}
	g.AddEdge("A", "B", 1, edgeData)

	// The edge should exist.
	if !g.HasEdge("A", "B") {
		t.Error("expected edge A->B to exist")
	}

	// But since we did not explicitly add "B", it should NOT be registered as a node.
	if g.HasNode("B") {
		t.Error("expected 'B' not to be auto-added as a node")
	}

	// GetNodes should not include "B".
	nodes := g.GetNodes()
	for _, n := range nodes {
		if n == "B" {
			t.Error("node 'B' should not be present in the node list")
		}
	}
}

func TestGraphRemoveEdge(t *testing.T) {
	t.Run("basic edge removal", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 2, edgeData)

		// Remove edge A->B
		err := g.RemoveEdge("A", "B")
		if err != nil {
			t.Errorf("unexpected error removing edge: %v", err)
		}

		// Verify edge was removed
		if g.HasEdge("A", "B") {
			t.Error("edge A->B should not exist after removal")
		}

		// Verify other edge still exists
		if !g.HasEdge("B", "C") {
			t.Error("edge B->C should still exist")
		}
	})

	t.Run("remove non-existent edge", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		err := g.RemoveEdge("A", "B")
		if err == nil {
			t.Error("expected error when removing non-existent edge")
		}
	})

	t.Run("remove edge with non-existent nodes", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")

		err := g.RemoveEdge("A", "B")
		if err == nil {
			t.Error("expected error when removing edge with non-existent destination")
		}

		err = g.RemoveEdge("B", "A")
		if err == nil {
			t.Error("expected error when removing edge with non-existent source")
		}
	})

	t.Run("concurrent edge removal", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			_ = g.RemoveEdge("A", "B")
		}()

		go func() {
			defer wg.Done()
			_ = g.RemoveEdge("A", "B")
		}()

		wg.Wait()
		if g.HasEdge("A", "B") {
			t.Error("edge A->B should not exist after concurrent removal attempts")
		}
	})
}

func TestGraphClone(t *testing.T) {
	t.Run("basic graph cloning", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		g.AddNode("C")

		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)
		g.AddEdge("B", "C", 2, edgeData)

		// Clone the graph
		cloned := g.Clone()

		// Verify nodes exist in cloned graph
		nodes := []string{"A", "B", "C"}
		for _, node := range nodes {
			if !cloned.HasNode(node) {
				t.Errorf("cloned graph missing node %s", node)
			}
		}

		// Verify edges and their data
		edges := [][2]string{{"A", "B"}, {"B", "C"}}
		for _, edge := range edges {
			from, to := edge[0], edge[1]
			if !cloned.HasEdge(from, to) {
				t.Errorf("cloned graph missing edge %s->%s", from, to)
			}

			originalEdge, _ := g.GetEdge(from, to)
			clonedEdge, _ := cloned.GetEdge(from, to)

			if originalEdge.Weight != clonedEdge.Weight {
				t.Errorf("edge weight mismatch for %s->%s: expected %d, got %d",
					from, to, originalEdge.Weight, clonedEdge.Weight)
			}

			if originalEdge.Data.Label != clonedEdge.Data.Label {
				t.Errorf("edge data label mismatch for %s->%s: expected %s, got %s",
					from, to, originalEdge.Data.Label, clonedEdge.Data.Label)
			}

			if originalEdge.Data.Cost != clonedEdge.Data.Cost {
				t.Errorf("edge data cost mismatch for %s->%s: expected %f, got %f",
					from, to, originalEdge.Data.Cost, clonedEdge.Data.Cost)
			}
		}
	})

	t.Run("independence of cloned graph", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")

		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)

		// Clone the graph
		cloned := g.Clone()

		// Modify original graph
		g.AddNode("C")
		g.AddEdge("B", "C", 2, TestEdgeData{Label: "new", Cost: 2.0})
		_ = g.RemoveEdge("A", "B")

		// Verify cloned graph remains unchanged
		if cloned.HasNode("C") {
			t.Error("cloned graph should not have new node C")
		}

		if !cloned.HasEdge("A", "B") {
			t.Error("cloned graph should still have edge A->B")
		}

		edge, _ := cloned.GetEdge("A", "B")
		if edge.Data.Label != "test" || edge.Data.Cost != 1.0 {
			t.Error("edge data in cloned graph should remain unchanged")
		}
	})

	t.Run("clone empty graph", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		cloned := g.Clone()

		if len(cloned.GetNodes()) != 0 {
			t.Error("cloned empty graph should have no nodes")
		}
	})

	t.Run("concurrent cloning", func(t *testing.T) {
		g := New[string, TestEdgeData]()
		g.AddNode("A")
		g.AddNode("B")
		edgeData := TestEdgeData{Label: "test", Cost: 1.0}
		g.AddEdge("A", "B", 1, edgeData)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cloned := g.Clone()
				if !cloned.HasEdge("A", "B") {
					t.Error("cloned graph missing edge A->B")
				}
			}()
		}
		wg.Wait()
	})
}
