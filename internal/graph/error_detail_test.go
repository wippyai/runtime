package graph

import (
	"errors"
	"strings"
	"testing"

	apierror "github.com/wippyai/runtime/api/error"
)

// Test that verifies the error contains detailed node information via Details()
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

	// Check that details contain cycle information
	var graphErr apierror.Error
	if !errors.As(err, &graphErr) {
		t.Fatalf("expected *Error, got %T", err)
	}

	details := graphErr.Details()
	if details == nil {
		t.Fatal("expected error details")
	}

	// Check for cycle or stuck_nodes in details
	_, hasCycle := details.Get("cycle")
	_, hasStuck := details.Get("stuck_nodes")
	if !hasCycle && !hasStuck {
		t.Errorf("error details should contain cycle info, got: %v", details)
	}

	t.Logf("Error message: %s, details: %v", errMsg, details)
}

// Test to verify error format and details when cycle is detected
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

	// Should contain "cycle detected"
	if !strings.Contains(errMsg, "cycle detected") {
		t.Errorf("error should contain 'cycle detected', got: %s", errMsg)
	}

	// Check details contain cycle information
	var graphErr apierror.Error
	if !errors.As(err, &graphErr) {
		t.Fatalf("expected *Error, got %T", err)
	}

	details := graphErr.Details()
	if details == nil {
		t.Fatal("expected error details")
	}

	t.Logf("Cycle error format: %s, details: %v", errMsg, details)
}

// Verify that errors for different cycle patterns are detected correctly
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

			// Check error has details
			var graphErr apierror.Error
			if errors.As(err, &graphErr) {
				details := graphErr.Details()
				t.Logf("[%s] Error: %s, details: %v", tc.name, errMsg, details)
			} else {
				t.Logf("[%s] Error: %s", tc.name, errMsg)
			}
		})
	}
}
