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

func TestGraphCache(t *testing.T) {
	g := NewGraph()
	g.AddNode("A")
	g.AddNode("B")
	g.AddEdge(Edge{From: "A", To: "B", Weight: 1})

	// Calculate and cache the shortest path
	path1, _ := g.ShortestPath("A", "B")

	// Modify the graph - this should invalidate the cache
	g.AddEdge(Edge{From: "A", To: "B", Weight: 2})

	// Get the path again - should NOT be from the cache
	path2, _ := g.ShortestPath("A", "B")

	if reflect.DeepEqual(path1, path2) {
		t.Errorf(
			"Cached %v path was returned after modification. Got %v, should have recalculated",
			path1,
			path2,
		)
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
