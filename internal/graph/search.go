package graph

import (
	"container/heap"
	"fmt"
)

// Path represents a sequence of nodes and their total accumulated cost in a graph.
// The Nodes slice contains the ordered sequence of nodes in the path, and Cost
// represents the sum of all edge weights along the path.
type Path[T comparable] struct {
	Nodes []T
	Cost  int
}

// priorityQueue implements the container/heap interface for Dijkstra's algorithm.
// It maintains a queue of items sorted by their priority (distance) values.
type priorityQueue[T comparable] struct {
	items []*item[T]
}

// item represents a node in the priority queue with its associated priority value.
// The index field is maintained by the heap interface implementation.
type item[T comparable] struct {
	node     T
	priority int
	index    int
}

// Len returns the number of items in the priority queue.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Len() int { return len(pq.items) }

// Less reports whether the item with index i should sort before the item with index j.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Less(i, j int) bool {
	return pq.items[i].priority < pq.items[j].priority
}

// Swap swaps the items at indices i and j.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

// Push adds an item to the priority queue.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Push(x interface{}) {
	n := len(pq.items)
	item := x.(*item[T])
	item.index = n
	pq.items = append(pq.items, item)
}

// Pop removes and returns the minimum item (according to Less) from the priority queue.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Pop() interface{} {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	pq.items = old[0 : n-1]
	return item
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
