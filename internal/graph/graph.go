package graph

import (
	"container/heap"
	"fmt"
	"sort"
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

// DependencyLevels returns the dependency levels of the graph organized in topological order.
// Each level contains nodes that only depend on nodes in previous levels.
// Returns an error if the graph contains a cycle, as cyclic dependencies cannot
// be organized into levels.
func (g *Graph[T]) DependencyLevels() (*DependencyLevels[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Calculate in-degree for each node
	inDegree := make(map[T]int)
	for node := range g.nodes {
		inDegree[node] = 0
	}
	for _, edges := range g.edges {
		for to := range edges {
			inDegree[to]++
		}
	}

	result := &DependencyLevels[T]{
		levels: make([][]T, 0),
	}

	// Continue until all nodes are processed
	foundNodes := true
	for len(inDegree) > 0 && foundNodes {
		// Find all nodes with no dependencies (in-degree = 0)
		currentLevel := make([]T, 0)
		foundNodes = false

		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
				foundNodes = true
			}
		}

		// If we have nodes but none with in-degree 0, we have a cycle
		if !foundNodes && len(inDegree) > 0 {
			remaining := make([]T, 0, len(inDegree))
			for node := range inDegree {
				remaining = append(remaining, node)
			}
			// Sort for stable error message
			sort.Slice(remaining, func(i, j int) bool {
				return fmt.Sprintf("%v", remaining[i]) < fmt.Sprintf("%v", remaining[j])
			})
			return nil, fmt.Errorf("cycle detected with nodes: %v", remaining)
		}

		// Remove current level nodes from consideration
		for _, node := range currentLevel {
			if edges, exists := g.edges[node]; exists {
				for neighbor := range edges {
					inDegree[neighbor]--
				}
			}
			delete(inDegree, node)
		}

		// Sort current level for consistent output
		sort.Slice(currentLevel, func(i, j int) bool {
			return fmt.Sprintf("%v", currentLevel[i]) < fmt.Sprintf("%v", currentLevel[j])
		})

		if len(currentLevel) > 0 {
			result.levels = append(result.levels, currentLevel)
		}
	}

	return result, nil
}

// ShortestPath finds the shortest path between two nodes using Dijkstra's algorithm.
// It returns a Path containing the sequence of nodes and the total cost of the path.
// The algorithm assumes all edge weights are non-negative.
//
// Parameters:
//   - from: The starting node
//   - to: The destination node
//
// Returns:
//   - *Path[T]: A path object containing the sequence of nodes and total cost
//   - error: An error if no path exists or if either node is missing from the graph
//
// The returned path will be the shortest possible path by total edge weight.
// If multiple paths have the same total weight, the algorithm makes no guarantees
// about which one will be returned.
func (g *Graph[T]) ShortestPath(from, to T) (*Path[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.HasNode(from) {
		return nil, fmt.Errorf("start node %v does not exist", from)
	}
	if !g.HasNode(to) {
		return nil, fmt.Errorf("end node %v does not exist", to)
	}

	// Initialize Dijkstra's algorithm data structures
	distances := make(map[T]int)
	previous := make(map[T]T)
	pq := &priorityQueue[T]{items: make([]*item[T], 0)}
	heap.Init(pq)

	// Set initial distances
	for node := range g.nodes {
		if node == from {
			distances[node] = 0
			heap.Push(pq, &item[T]{node: node, priority: 0})
		} else {
			distances[node] = -1 // -1 represents infinity
		}
	}

	// Process nodes until queue is empty
	for pq.Len() > 0 {
		current := heap.Pop(pq).(*item[T])

		// Skip if we've found a better path already
		if current.priority > distances[current.node] {
			continue
		}

		// Process all neighbors
		for neighbor, weight := range g.edges[current.node] {
			newDist := distances[current.node] + weight

			// Update distance if we found a better path
			if distances[neighbor] == -1 || newDist < distances[neighbor] {
				distances[neighbor] = newDist
				previous[neighbor] = current.node
				heap.Push(pq, &item[T]{
					node:     neighbor,
					priority: newDist,
				})
			}
		}
	}

	// Check if we found a path to destination
	if distances[to] == -1 {
		return nil, fmt.Errorf("no path exists from %v to %v", from, to)
	}

	// Build path
	path := &Path[T]{
		Cost:  distances[to],
		Nodes: make([]T, 0),
	}

	// Reconstruct path from destination to source
	for current := to; ; current = previous[current] {
		path.Nodes = append([]T{current}, path.Nodes...)
		if current == from {
			break
		}
	}

	return path, nil
}
