package graph

import (
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestGraph(t *testing.T) {
	g := NewGraph()

	// Add nodes
	g.AddNode("A")
	g.AddNode("B")
	g.AddNode("C")
	g.AddNode("D")
	g.AddNode("E")
	g.AddNode("F") // Added for more complex tests

	// Add edges
	g.AddEdge(Edge{From: "A", To: "B", Weight: 4})
	g.AddEdge(Edge{From: "A", To: "C", Weight: 2})
	g.AddEdge(Edge{From: "B", To: "E", Weight: 3})
	g.AddEdge(Edge{From: "C", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "C", To: "D", Weight: 5})
	g.AddEdge(Edge{From: "D", To: "E", Weight: 1})
	g.AddEdge(Edge{From: "E", To: "F", Weight: 2})  // Edge to F
	g.AddEdge(Edge{From: "A", To: "F", Weight: 10}) // Direct edge to F

	tests := []struct {
		name    string
		from    Node
		to      Node
		want    *Path
		wantErr bool
	}{
		{
			name:    "A to E",
			from:    "A",
			to:      "E",
			want:    &Path{Nodes: []Node{"A", "C", "B", "E"}, Cost: 6},
			wantErr: false,
		},
		{
			name:    "A to D",
			from:    "A",
			to:      "D",
			want:    &Path{Nodes: []Node{"A", "C", "D"}, Cost: 7},
			wantErr: false,
		},
		{
			name:    "E to A",
			from:    "E",
			to:      "A",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "A to A",
			from:    "A",
			to:      "A",
			want:    &Path{Nodes: []Node{"A"}, Cost: 0},
			wantErr: false,
		},
		{
			name:    "A to F",
			from:    "A",
			to:      "F",
			want:    &Path{Nodes: []Node{"A", "C", "B", "E", "F"}, Cost: 8},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := g.ShortestPath(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("Graph.ShortestPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Graph.ShortestPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGraphConcurrency(t *testing.T) {
	g := NewGraph()
	nodes := []Node{"A", "B", "C", "D", "E", "F"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	edges := []Edge{
		{From: "A", To: "B", Weight: 4},
		{From: "A", To: "C", Weight: 2},
		{From: "B", To: "E", Weight: 3},
		{From: "C", To: "B", Weight: 1},
		{From: "C", To: "D", Weight: 5},
		{From: "D", To: "E", Weight: 1},
		{From: "E", To: "F", Weight: 2},
		{From: "A", To: "F", Weight: 10},
	}

	for _, edge := range edges {
		g.AddEdge(edge)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = g.ShortestPath("A", "F")
			_, _ = g.ShortestPath("A", "E")
		}()
	}
	wg.Wait()

	// Verify final result after concurrent access
	expectedPath := &Path{Nodes: []Node{"A", "C", "B", "E", "F"}, Cost: 8}
	actualPath, err := g.ShortestPath("A", "F")

	if err != nil {
		t.Errorf("Error getting shortest path after concurrent access: %v", err)
	}

	if !reflect.DeepEqual(actualPath, expectedPath) {
		t.Errorf("Incorrect shortest path after concurrent access. Got: %v, Expected: %v", actualPath, expectedPath)
	}
}

func TestGraph_NoPath(t *testing.T) {
	g := NewGraph()
	g.AddNode("A")
	g.AddNode("B")
	// No edges added

	_, err := g.ShortestPath("A", "B")
	if err == nil {
		t.Errorf("ShortestPath() should have returned an error for disconnected nodes")
	}
}

func TestGraph_MissingNode(t *testing.T) {
	g := NewGraph()
	// No nodes added

	_, err := g.ShortestPath("A", "B")
	if err == nil {
		t.Errorf("ShortestPath() should have returned an error for missing nodes")
	}
}

func TestGraph_GetNodes(t *testing.T) {
	g := NewGraph()

	// Add some nodes
	nodes := []Node{"A", "B", "C"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	// Get all nodes
	got := g.GetNodes()

	// Since map iteration order is random, we need to sort both slices before comparing
	sort.Slice(got, func(i, j int) bool { return string(got[i]) < string(got[j]) })
	if !reflect.DeepEqual(got, nodes) {
		t.Errorf("GetNodes() = %v, want %v", got, nodes)
	}
}

func TestGraph_GetEdges(t *testing.T) {
	g := NewGraph()

	// Add nodes and edges
	g.AddNode("A")
	g.AddNode("B")
	g.AddNode("C")

	expectedEdges := []Edge{
		{From: "A", To: "B", Weight: 1},
		{From: "B", To: "C", Weight: 2},
	}

	for _, edge := range expectedEdges {
		g.AddEdge(edge)
	}

	// Get all edges
	got := g.GetEdges()

	// Sort both slices for comparison (since map iteration order is random)
	sort.Slice(got, func(i, j int) bool {
		if got[i].From != got[j].From {
			return string(got[i].From) < string(got[j].From)
		}
		return string(got[i].To) < string(got[j].To)
	})
	sort.Slice(expectedEdges, func(i, j int) bool {
		if expectedEdges[i].From != expectedEdges[j].From {
			return string(expectedEdges[i].From) < string(expectedEdges[j].From)
		}
		return string(expectedEdges[i].To) < string(expectedEdges[j].To)
	})

	if !reflect.DeepEqual(got, expectedEdges) {
		t.Errorf("GetEdges() = %v, want %v", got, expectedEdges)
	}
}
func TestGraph_RemoveNode(t *testing.T) {
	g := NewGraph()

	// Test removing non-existent node
	err := g.RemoveNode("A")
	if err == nil {
		t.Error("RemoveNode() should return error for non-existent node")
	}

	// Add nodes and edges
	g.AddNode("A")
	g.AddNode("B")
	g.AddNode("C")
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "B", To: "C", Weight: 2})
	g.AddEdge(Edge{From: "C", To: "A", Weight: 3})

	// Test removing node B (middle node)
	err = g.RemoveNode("B")
	if err != nil {
		t.Errorf("RemoveNode() error = %v, want nil", err)
	}

	// Verify B is removed
	if g.HasEdge("A", "B") {
		t.Error("Edge A->B still exists after removing node B")
	}
	if g.HasEdge("B", "C") {
		t.Error("Edge B->C still exists after removing node B")
	}

	// Verify other edges/nodes remain
	if !g.HasEdge("C", "A") {
		t.Error("Edge C->A should still exist")
	}

	// Test removing node with no edges
	g.AddNode("D")
	err = g.RemoveNode("D")
	if err != nil {
		t.Errorf("RemoveNode() error = %v for node with no edges", err)
	}

	// Verify removal from nodes map
	nodes := g.GetNodes()
	for _, node := range nodes {
		if node == "B" || node == "D" {
			t.Errorf("Node %s still exists after removal", node)
		}
	}

	// Test removing last node
	for _, node := range g.GetNodes() {
		err = g.RemoveNode(node)
		if err != nil {
			t.Errorf("RemoveNode() error = %v when removing last nodes", err)
		}
	}

	// Verify graph is empty
	if len(g.GetNodes()) != 0 {
		t.Error("Graph should be empty after removing all nodes")
	}
	if len(g.GetEdges()) != 0 {
		t.Error("Graph should have no edges after removing all nodes")
	}
}

func TestGraph_RemoveEdges(t *testing.T) {
	g := NewGraph()

	// Test removing edges between non-existent nodes
	err := g.RemoveEdges("A", "B")
	if err == nil {
		t.Error("RemoveEdges() should return error for non-existent nodes")
	}

	// Setup test graph
	g.AddNode("A")
	g.AddNode("B")
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "B", To: "A", Weight: 2})

	// Test removing edges in both directions
	err = g.RemoveEdges("A", "B")
	if err != nil {
		t.Errorf("RemoveEdges() error = %v", err)
	}

	// Verify both edges are removed
	if g.HasEdge("A", "B") || g.HasEdge("B", "A") {
		t.Error("Edges still exist after RemoveEdges()")
	}

	// Test removing edges when only one direction exists
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	err = g.RemoveEdges("A", "B")
	if err != nil {
		t.Errorf("RemoveEdges() error = %v for one-way edge", err)
	}
	if g.HasEdge("A", "B") || g.HasEdge("B", "A") {
		t.Error("Edge still exists after RemoveEdges() for one-way edge")
	}

	// Test empty edge maps cleanup
	if _, exists := g.edges["A"]; exists && len(g.edges["A"]) == 0 {
		t.Error("Empty edge map not cleaned up")
	}
}

func TestGraph_GetNeighbors(t *testing.T) {
	g := NewGraph()
	g.AddNode("A")
	g.AddNode("B")
	g.AddNode("C")
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})
	g.AddEdge(Edge{From: "A", To: "C", Weight: 2})
	g.AddEdge(Edge{From: "B", To: "C", Weight: 3})

	tests := []struct {
		name      string
		node      Node
		wantNodes []Node
		wantErr   bool
	}{
		{
			name:      "node with multiple neighbors",
			node:      "A",
			wantNodes: []Node{"B", "C"},
			wantErr:   false,
		},
		{
			name:      "node with one neighbor",
			node:      "B",
			wantNodes: []Node{"C"},
			wantErr:   false,
		},
		{
			name:      "node with no neighbors",
			node:      "C",
			wantNodes: []Node{},
			wantErr:   false,
		},
		{
			name:      "non-existent node",
			node:      "X",
			wantNodes: nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := g.GetNeighbors(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetNeighbors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				sort.Slice(got, func(i, j int) bool {
					return string(got[i]) < string(got[j])
				})
				sort.Slice(tt.wantNodes, func(i, j int) bool {
					return string(tt.wantNodes[i]) < string(tt.wantNodes[j])
				})
				if !reflect.DeepEqual(got, tt.wantNodes) {
					t.Errorf("GetNeighbors() = %v, want %v", got, tt.wantNodes)
				}
			}
		})
	}
}
