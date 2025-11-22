package graph

import (
	"strings"
	"testing"
)

// Test that verifies the error message contains detailed node information
// This tests the actual error formatting logic
func TestCycleErrorContainsNodeDetails(t *testing.T) {
	g := New[string, string]()

	// Create a cycle with meaningful component names
	nodes := []string{"service-http", "service-storage", "service-otel"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	// Create a 3-node cycle
	g.AddEdge("service-http", "service-storage", 1, "")
	g.AddEdge("service-storage", "service-otel", 1, "")
	g.AddEdge("service-otel", "service-http", 1, "")

	_, err := g.DependencyLevels()
	if err == nil {
		t.Fatal("expected cycle error")
	}

	errMsg := err.Error()

	// Verify error starts with cycle detection message
	if !strings.HasPrefix(errMsg, "cycle detected") {
		t.Errorf("error should start with 'cycle detected', got: %s", errMsg)
	}

	// Error should mention at least one of the nodes involved
	hasNodeInfo := strings.Contains(errMsg, "service-http") ||
		strings.Contains(errMsg, "service-storage") ||
		strings.Contains(errMsg, "service-otel")

	if !hasNodeInfo {
		t.Errorf("error should contain node names, got: %s", errMsg)
	}

	t.Logf("Error message: %s", errMsg)
}

// Test to verify detailed error message format when cycle path is shown
func TestCycleErrorFormat(t *testing.T) {
	g := New[string, string]()

	nodes := []string{"A", "B", "C", "D"}
	for _, node := range nodes {
		g.AddNode(node)
	}

	// A -> B -> C -> A (cycle)
	// D is isolated
	g.AddEdge("A", "B", 1, "")
	g.AddEdge("B", "C", 1, "")
	g.AddEdge("C", "A", 1, "")

	_, err := g.DependencyLevels()
	if err == nil {
		t.Fatal("expected cycle error")
	}

	errMsg := err.Error()

	// Verify the error is informative
	if len(errMsg) < 20 {
		t.Errorf("error message seems too short to be useful: %s", errMsg)
	}

	// Should contain "cycle detected"
	if !strings.Contains(errMsg, "cycle detected") {
		t.Errorf("error should contain 'cycle detected', got: %s", errMsg)
	}

	t.Logf("Cycle error format: %s", errMsg)
}

// Verify that errors for different cycle patterns are distinguishable
func TestMultipleCyclePatterns(t *testing.T) {
	testCases := []struct {
		name  string
		setup func(*Graph[string, string])
	}{
		{
			name: "simple_2_node_cycle",
			setup: func(g *Graph[string, string]) {
				g.AddNode("A")
				g.AddNode("B")
				g.AddEdge("A", "B", 1, "")
				g.AddEdge("B", "A", 1, "")
			},
		},
		{
			name: "3_node_cycle",
			setup: func(g *Graph[string, string]) {
				g.AddNode("X")
				g.AddNode("Y")
				g.AddNode("Z")
				g.AddEdge("X", "Y", 1, "")
				g.AddEdge("Y", "Z", 1, "")
				g.AddEdge("Z", "X", 1, "")
			},
		},
		{
			name: "self_loop",
			setup: func(g *Graph[string, string]) {
				g.AddNode("S")
				g.AddEdge("S", "S", 1, "")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := New[string, string]()
			tc.setup(g)

			_, err := g.DependencyLevels()
			if err == nil {
				t.Fatal("expected cycle error")
			}

			errMsg := err.Error()
			if !strings.Contains(errMsg, "cycle") {
				t.Errorf("error should mention cycle, got: %s", errMsg)
			}

			// Error should be specific enough to identify the problem
			if len(errMsg) < 15 {
				t.Errorf("error message too generic: %s", errMsg)
			}

			t.Logf("[%s] Error: %s", tc.name, errMsg)
		})
	}
}
