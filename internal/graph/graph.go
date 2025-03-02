// package graph

package graph

import (
	"container/heap"
	"fmt"
	"sort"
	"sync"
)

// Edge represents a custom edge type that can store arbitrary properties.
type Edge[T comparable, E any] struct {
	To     T
	Weight int
	Data   E
}

// Graph represents a generic directed graph with custom edge types.
type Graph[T comparable, E any] struct {
	nodes map[T]bool
	edges map[T]map[T]Edge[T, E]
	mu    sync.RWMutex
}

// New creates and returns a new empty directed graph.
func New[T comparable, E any]() *Graph[T, E] {
	return &Graph[T, E]{
		nodes: make(map[T]bool),
		edges: make(map[T]map[T]Edge[T, E]),
	}
}

// AddNode adds a new node to the graph.
// If the node already exists, it will not modify the graph.
func (g *Graph[T, E]) AddNode(n T) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n] = true
}

// AddEdge adds a directed edge with custom data.
// NOTE: Unlike the previous (new) implementation, it does NOT auto-add missing nodes.
// This reverts the behavior to that of the older graph package.
func (g *Graph[T, E]) AddEdge(from, to T, weight int, data E) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, ok := g.edges[from]; !ok {
		g.edges[from] = make(map[T]Edge[T, E])
	}
	g.edges[from][to] = Edge[T, E]{
		To:     to,
		Weight: weight,
		Data:   data,
	}
}

// RemoveNode removes the specified node and all its associated edges from the graph.
// It returns an error if the node doesn't exist.
func (g *Graph[T, E]) RemoveNode(n T) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.nodes[n] {
		return fmt.Errorf("node %v does not exist", n)
	}

	delete(g.nodes, n)
	delete(g.edges, n)

	// Done all edges pointing to this node.
	for from, edges := range g.edges {
		delete(edges, n)
		if len(edges) == 0 {
			delete(g.edges, from)
		}
	}

	return nil
}

// HasNode returns true if the specified node exists in the graph.
func (g *Graph[T, E]) HasNode(n T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.nodes[n]
}

// HasEdge returns true if there exists a directed edge from the 'from' node to the 'to' node.
func (g *Graph[T, E]) HasEdge(from, to T) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if edges, exists := g.edges[from]; exists {
		_, hasEdge := edges[to]
		return hasEdge
	}
	return false
}

// GetEdge returns the edge data between two nodes if it exists.
func (g *Graph[T, E]) GetEdge(from, to T) (Edge[T, E], bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if edges, exists := g.edges[from]; exists {
		if edge, hasEdge := edges[to]; hasEdge {
			return edge, true
		}
	}
	return Edge[T, E]{}, false
}

// RemoveEdge removes a directed edge from the 'from' node to the 'to' node.
// It returns an error if either node doesn't exist or if the edge doesn't exist.
func (g *Graph[T, E]) RemoveEdge(from, to T) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check if both nodes exist
	if !g.nodes[from] {
		return fmt.Errorf("source node %v does not exist", from)
	}
	if !g.nodes[to] {
		return fmt.Errorf("destination node %v does not exist", to)
	}

	// Check if the edge exists
	edges, exists := g.edges[from]
	if !exists {
		return fmt.Errorf("no edges exist from node %v", from)
	}

	if _, hasEdge := edges[to]; !hasEdge {
		return fmt.Errorf("edge from %v to %v does not exist", from, to)
	}

	// Done the edge
	delete(g.edges[from], to)

	// If this was the last edge from the source node, clean up the empty map
	if len(g.edges[from]) == 0 {
		delete(g.edges, from)
	}

	// todo; test it
	return nil
}

// GetNodes returns a slice containing all nodes currently in the graph.
// The order of nodes in the returned slice is not guaranteed.
func (g *Graph[T, E]) GetNodes() []T {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodes := make([]T, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// Clone creates a deep copy of the graph.
// The new graph contains copies of all nodes and edges with their associated data.
func (g *Graph[T, E]) Clone() *Graph[T, E] {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Spawn a new graph
	cloned := New[T, E]()

	// Copy nodes
	for node := range g.nodes {
		cloned.nodes[node] = true
	}

	// Copy edges and their data
	for from, edges := range g.edges {
		cloned.edges[from] = make(map[T]Edge[T, E])
		for to, edge := range edges {
			cloned.edges[from][to] = Edge[T, E]{
				To:     edge.To,
				Weight: edge.Weight,
				Data:   edge.Data,
			}
		}
	}

	return cloned
}

// GetNeighbors returns a slice containing all nodes that have outgoing edges from the specified node.
// Returns an error if the specified node doesn't exist in the graph.
// Returns an empty slice if the node has no outgoing edges.
func (g *Graph[T, E]) GetNeighbors(n T) ([]T, error) {
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
func (g *Graph[T, E]) DependencyLevels() (*DependencyLevels[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Calculate in-degree for each node.
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

	// Continue until all nodes are processed.
	foundNodes := true
	for len(inDegree) > 0 && foundNodes {
		// Find all nodes with no dependencies (in-degree = 0).
		currentLevel := make([]T, 0)
		foundNodes = false

		for node, degree := range inDegree {
			if degree == 0 {
				currentLevel = append(currentLevel, node)
				foundNodes = true
			}
		}

		// If we have nodes but none with in-degree 0, we have a cycle.
		if !foundNodes && len(inDegree) > 0 {
			remaining := make([]T, 0, len(inDegree))
			for node := range inDegree {
				remaining = append(remaining, node)
			}
			// Sort for stable error message.
			sort.Slice(remaining, func(i, j int) bool {
				return fmt.Sprintf("%v", remaining[i]) < fmt.Sprintf("%v", remaining[j])
			})
			return nil, fmt.Errorf("cycle detected with nodes: %v", remaining)
		}

		// Done current level nodes from consideration.
		for _, node := range currentLevel {
			if edges, exists := g.edges[node]; exists {
				for neighbor := range edges {
					inDegree[neighbor]--
				}
			}
			delete(inDegree, node)
		}

		// Sort current level for consistent output.
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
func (g *Graph[T, E]) ShortestPath(from, to T) (*Path[T], error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if !g.HasNode(from) {
		return nil, fmt.Errorf("start node %v does not exist", from)
	}
	if !g.HasNode(to) {
		return nil, fmt.Errorf("end node %v does not exist", to)
	}

	// Initialize Dijkstra's algorithm data structures.
	distances := make(map[T]int)
	previous := make(map[T]T)
	pq := &priorityQueue[T]{items: make([]*item[T], 0)}
	heap.Init(pq)

	// Set initial distances.
	for node := range g.nodes {
		if node == from {
			distances[node] = 0
			heap.Push(pq, &item[T]{node: node, priority: 0})
		} else {
			distances[node] = -1 // -1 represents infinity.
		}
	}

	// Process nodes until queue is empty.
	for pq.Len() > 0 {
		current := heap.Pop(pq).(*item[T])

		// Skip if we've found a better path already.
		if current.priority > distances[current.node] {
			continue
		}

		// Process all neighbors.
		for neighbor, edge := range g.edges[current.node] {
			newDist := distances[current.node] + edge.Weight

			// Update distance if we found a better path.
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

	// Check if we found a path to destination.
	if distances[to] == -1 {
		return nil, fmt.Errorf("no path exists from %v to %v", from, to)
	}

	// Build path.
	path := &Path[T]{
		Cost:  distances[to],
		Nodes: make([]T, 0),
	}

	// Reconstruct path from destination to source.
	for current := to; ; current = previous[current] {
		path.Nodes = append([]T{current}, path.Nodes...)
		if current == from {
			break
		}
	}

	return path, nil
}
