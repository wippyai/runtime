package graph

import (
	"fmt"
	"sync"
)

// Node represents a vertex in the graph
type Node string

// Edge represents a directed edge between two nodes with a weight
type Edge struct {
	From   Node
	To     Node
	Weight int
}

// Path represents a sequence of nodes and their total cost
type Path struct {
	Nodes []Node
	Cost  int
}

// Graph represents a weighted directed graph with concurrent access support
type Graph struct {
	nodes map[Node]bool
	edges map[Node]map[Node]int
	mutex sync.RWMutex
}

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
			distances[node] = -1 // Using -1 to represent infinity
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

		if distances[current.node] == -1 {
			break // No path exists to remaining nodes
		}

		// Check all neighbors of current node
		for neighbor, weight := range g.edges[current.node] {
			newDistance := distances[current.node] + weight

			if distances[neighbor] == -1 || newDistance < distances[neighbor] {
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
	if distances[to] == -1 {
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
