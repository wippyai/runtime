// SPDX-License-Identifier: MPL-2.0

package graph

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
func (pq *priorityQueue[T]) Push(x any) {
	n := len(pq.items)
	item := x.(*item[T])
	item.index = n
	pq.items = append(pq.items, item)
}

// Pop removes and returns the minimum item (according to Less) from the priority queue.
// Implements part of heap.Interface.
func (pq *priorityQueue[T]) Pop() any {
	old := pq.items
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	pq.items = old[0 : n-1]
	return item
}
