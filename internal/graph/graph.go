package graph

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"
)

// Node represents a data type in the graph.
type Node string

// Edge represents a possible transcoding operation between two types.
type Edge struct {
	From   Node
	To     Node
	Weight int
}

// Graph represents the network of data types and their transcoding relationships.
type Graph struct {
	Nodes     map[Node]bool
	Edges     map[Node]map[Node]int // Adjacency list: Node -> (Node -> Weight)
	mutex     sync.RWMutex          // For concurrent access
	cache     map[string]*Path      // Cache for shortest paths
	cacheLock sync.RWMutex
}

// Path represents a sequence of nodes forming a path.
type Path struct {
	Nodes []Node
	Cost  int
}

// NewGraph creates a new graph.
func NewGraph() *Graph {
	return &Graph{
		Nodes: make(map[Node]bool),
		Edges: make(map[Node]map[Node]int),
		cache: make(map[string]*Path),
	}
}

// AddNode adds a node to the graph.
func (g *Graph) AddNode(n Node) {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	g.Nodes[n] = true
	g.invalidateCache() // Invalidate cache on node addition
}

// AddEdge adds a directed edge to the graph.
func (g *Graph) AddEdge(e Edge) {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	if _, ok := g.Edges[e.From]; !ok {
		g.Edges[e.From] = make(map[Node]int)
	}
	g.Edges[e.From][e.To] = e.Weight
	g.invalidateCache() // Invalidate cache on edge addition
}

// priorityQueueItem represents an item in the priority queue used by Dijkstra's algorithm.
type priorityQueueItem struct {
	node     Node
	priority int
	index    int // Index in the heap (needed for updating priority)
}

// priorityQueue is a priority queue implementation using a min-heap.
type priorityQueue []*priorityQueueItem

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].priority < pq[j].priority }
func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *priorityQueue) Push(x interface{}) {
	n := len(*pq)
	item := x.(*priorityQueueItem)
	item.index = n
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // Avoid memory leak
	item.index = -1 // For safety
	*pq = old[0 : n-1]
	return item
}

// cacheKey creates a key for the cache based on the source and destination nodes.
func cacheKey(from, to Node) string {
	return fmt.Sprintf("%s:%s", from, to)
}

// invalidateCache clears the shortest path cache.
func (g *Graph) invalidateCache() {
	g.cacheLock.Lock()
	defer g.cacheLock.Unlock()
	g.cache = make(map[string]*Path)
}

// ShortestPath calculates the shortest path between two nodes using Dijkstra's algorithm.
func (g *Graph) ShortestPath(from, to Node) (*Path, error) {
	// Check cache first
	g.cacheLock.RLock()
	if path, ok := g.cache[cacheKey(from, to)]; ok {
		g.cacheLock.RUnlock()
		return path, nil
	}
	g.cacheLock.RUnlock()

	g.mutex.RLock()
	defer g.mutex.RUnlock()

	// Dijkstra's algorithm
	distances := make(map[Node]int)
	previous := make(map[Node]Node)
	pq := make(priorityQueue, 0, len(g.Nodes))

	for node := range g.Nodes {
		if node == from {
			distances[node] = 0
			heap.Push(&pq, &priorityQueueItem{node: node, priority: 0})
		} else {
			distances[node] = -1 // -1 represents infinity for unvisited nodes
		}
	}

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*priorityQueueItem).node

		if distances[current] == -1 {
			break // All remaining nodes are unreachable
		}

		if current == to {
			break // Reached the destination
		}

		for neighbor, weight := range g.Edges[current] {
			alt := distances[current] + weight
			if distances[neighbor] == -1 || alt < distances[neighbor] {
				distances[neighbor] = alt
				previous[neighbor] = current
				// Update priority if neighbor is already in the queue
				found := false
				for i := 0; i < pq.Len(); i++ {
					if pq[i].node == neighbor {
						pq[i].priority = alt
						heap.Fix(&pq, pq[i].index)
						found = true
						break
					}
				}
				// Add neighbor to queue if not already present
				if !found {
					heap.Push(&pq, &priorityQueueItem{node: neighbor, priority: alt})
				}
			}
		}
	}

	if distances[to] == -1 {
		return nil, errors.New("no path found")
	}

	// Reconstruct path
	path := &Path{Cost: distances[to]}
	for node := to; node != ""; node = previous[node] {
		path.Nodes = append([]Node{node}, path.Nodes...)
	}

	// Store in cache
	g.cacheLock.Lock()
	g.cache[cacheKey(from, to)] = path
	g.cacheLock.Unlock()

	return path, nil
}
