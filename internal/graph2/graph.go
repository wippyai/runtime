package graph2

import (
	"fmt"
	"sync"
)

// Graph represents a generic directed graph with weighted edges.
// It is safe for concurrent use through its embedded mutex.
type Graph[T comparable] struct {
	nodes map[T]bool
	edges map[T]map[T]int
	mu    sync.RWMutex
}

// New creates and returns a new empty directed graph.
// The graph is initialized with empty node and edge maps.
func New[T comparable]() *Graph[T] {
	return &Graph[T]{
		nodes: make(map[T]bool),
		edges: make(map[T]map[T]int),
	}
}

// AddNode adds a new node to the graph.
// If the node already exists, it will not modify the graph.
func (g *Graph[T]) AddNode(n T) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n] = true
}

// AddEdge adds a directed edge from the 'from' node to the 'to' node with the specified weight.
// If the edge already exists, it will update the weight.
// If either node doesn't exist, it will be automatically added to the graph.
func (g *Graph[T]) AddEdge(from, to T, weight int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.edges[from]; !ok {
		g.edges[from] = make(map[T]int)
	}
	g.edges[from][to] = weight
}

// RemoveNode removes the specified node and all its associated edges from the graph.
// It returns an error if the node doesn't exist.
func (g *Graph[T]) RemoveNode(n T) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.nodes[n] {
		return fmt.Errorf("node %v does not exist", n)
	}

	delete(g.nodes, n)
	delete(g.edges, n)

	// Remove all edges pointing to this node
	for from, edges := range g.edges {
		delete(edges, n)
		if len(edges) == 0 {
			delete(g.edges, from)
		}
	}

	return nil
}

// HasNode returns true if the specified node exists in the graph.
func (g *Graph[T]) HasNode(n T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[n]
}

// HasEdge returns true if there exists a directed edge from the 'from' node to the 'to' node.
func (g *Graph[T]) HasEdge(from, to T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if edges, exists := g.edges[from]; exists {
		_, hasEdge := edges[to]
		return hasEdge
	}
	return false
}

// GetNodes returns a slice containing all nodes currently in the graph.
// The order of nodes in the returned slice is not guaranteed.
func (g *Graph[T]) GetNodes() []T {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]T, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetNeighbors returns a slice containing all nodes that have incoming edges from the specified node.
// Returns an error if the specified node doesn't exist in the graph.
// Returns an empty slice if the node has no outgoing edges.
func (g *Graph[T]) GetNeighbors(n T) ([]T, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.nodes[n] {
		return nil, fmt.Errorf("node %v does not exist", n)
	}

	edges, exists := g.edges[n]
	if !exists {
		return []T{}, nil
	}

	neighbors := make([]T, 0, len(edges))
	for neighbor := range edges {
		neighbors = append(neighbors, neighbor)
	}
	return neighbors, nil
}
