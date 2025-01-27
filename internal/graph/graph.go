package graph

import (
	"fmt"
	"sync"
)

// Infinity represents an infinite cost in the graph
const Infinity = -1

type (
	// Node represents a vertex in the graph
	Node string // todo: use template

	// Edge represents a directed edge between two nodes with a weight
	Edge struct {
		From   Node
		To     Node
		Weight int
	}

	// Path represents a sequence of nodes and their total cost
	Path struct {
		Nodes []Node
		Cost  int
	}

	// Graph represents a weighted directed graph with concurrent access support
	Graph struct {
		nodes map[Node]bool
		edges map[Node]map[Node]int
		mutex sync.RWMutex
	}
)

// NewGraph creates a new empty graph
func NewGraph() *Graph {
	return &Graph{
		nodes: make(map[Node]bool),
		edges: make(map[Node]map[Node]int),
	}
}

// AddNode adds a new node to the graph
func (g *Graph) AddNode(n Node) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.nodes[n] = true
}

// AddEdge adds a directed edge to the graph
func (g *Graph) AddEdge(e Edge) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if _, ok := g.edges[e.From]; !ok {
		g.edges[e.From] = make(map[Node]int)
	}
	g.edges[e.From][e.To] = e.Weight
}

// RemoveNode removes a node and all its associated edges from the graph.
// Returns error if the node doesn't exist.
func (g *Graph) RemoveNode(n Node) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Check if node exists
	if !g.nodes[n] {
		return fmt.Errorf("node %s does not exist", n)
	}

	// Remove the node from nodes map
	delete(g.nodes, n)

	// Remove all edges where this node is the source
	delete(g.edges, n)

	// Remove all edges where this node is the destination
	for source, edges := range g.edges {
		delete(edges, n)
		// If source node has no more edges, clean up the empty map
		if len(edges) == 0 {
			delete(g.edges, source)
		}
	}

	return nil
}

// RemoveEdges removes all edges between two nodes in both directions.
// Returns error if either node doesn't exist.
func (g *Graph) RemoveEdges(node1, node2 Node) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	// Verify both nodes exist
	if !g.nodes[node1] {
		return fmt.Errorf("node %s does not exist", node1)
	}
	if !g.nodes[node2] {
		return fmt.Errorf("node %s does not exist", node2)
	}

	// Remove edge from node1 to node2 if it exists
	if edges, exists := g.edges[node1]; exists {
		delete(edges, node2)
		if len(edges) == 0 {
			delete(g.edges, node1)
		}
	}

	// Remove edge from node2 to node1 if it exists
	if edges, exists := g.edges[node2]; exists {
		delete(edges, node1)
		if len(edges) == 0 {
			delete(g.edges, node2)
		}
	}

	return nil
}

// HasEdge checks if an edge exists between two nodes.
// Returns false if either node doesn't exist.
func (g *Graph) HasEdge(from, to Node) bool {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	// Check if both nodes exist
	if !g.nodes[from] || !g.nodes[to] {
		return false
	}

	// Check if edge exists
	if edges, exists := g.edges[from]; exists {
		_, hasEdge := edges[to]
		return hasEdge
	}
	return false
}

// GetNeighbors returns all nodes that have edges from the given node.
// Returns error if the node doesn't exist.
func (g *Graph) GetNeighbors(n Node) ([]Node, error) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	if !g.nodes[n] {
		return nil, fmt.Errorf("node %s does not exist", n)
	}

	edges, exists := g.edges[n]
	if !exists {
		return []Node{}, nil
	}

	neighbors := make([]Node, 0, len(edges))
	for neighbor := range edges {
		neighbors = append(neighbors, neighbor)
	}

	return neighbors, nil
}

// ShortestPath finds the shortest path between two nodes using Dijkstra's algorithm
func (g *Graph) ShortestPath(from, to Node) (*Path, error) {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	// Verify nodes exist
	if !g.nodes[from] {
		return nil, fmt.Errorf("start node %s does not exist", from)
	}
	if !g.nodes[to] {
		return nil, fmt.Errorf("end node %s does not exist", to)
	}

	// Initialize Dijkstra's algorithm data structures
	distances := make(map[Node]int)
	previous := make(map[Node]Node)
	pq := newPriorityQueue()

	// Initialize distances
	for node := range g.nodes {
		if node == from {
			distances[node] = 0
			pq.SafePush(newItem(node, 0))
		} else {
			distances[node] = Infinity
		}
	}

	// Main Dijkstra's algorithm loop
	for pq.Len() > 0 {
		current := pq.SafePop()
		if current == nil {
			break
		}

		if current.node == to {
			break // Reached destination
		}

		if distances[current.node] == Infinity {
			break // No path exists to remaining nodes
		}

		// Check all neighbors of current node
		for neighbor, weight := range g.edges[current.node] {
			newDistance := distances[current.node] + weight

			if distances[neighbor] == Infinity || newDistance < distances[neighbor] {
				distances[neighbor] = newDistance
				previous[neighbor] = current.node

				// Update or add neighbor to priority queue
				if existingItem := pq.Contains(neighbor); existingItem != nil {
					pq.UpdatePriority(existingItem, newDistance)
				} else {
					pq.SafePush(newItem(neighbor, newDistance))
				}
			}
		}
	}

	// Check if path exists
	if distances[to] == Infinity {
		return nil, fmt.Errorf("no path found")
	}

	// Reconstruct path
	path := &Path{
		Cost:  distances[to],
		Nodes: make([]Node, 0),
	}

	// Build path from destination to source
	for current := to; current != ""; current = previous[current] {
		path.Nodes = append([]Node{current}, path.Nodes...)
	}

	return path, nil
}

// GetNodes returns a slice of all nodes in the graph
func (g *Graph) GetNodes() []Node {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	nodes := make([]Node, 0, len(g.nodes))
	for node := range g.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// GetEdges returns all edges in the graph
func (g *Graph) GetEdges() []Edge {
	g.mutex.RLock()
	defer g.mutex.RUnlock()

	edges := make([]Edge, 0)
	for from, neighbors := range g.edges {
		for to, weight := range neighbors {
			edges = append(edges, Edge{
				From:   from,
				To:     to,
				Weight: weight,
			})
		}
	}
	return edges
}
