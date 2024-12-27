package graph

import "container/heap"

// priorityQueue implements a min-heap for Dijkstra's algorithm.
// It maintains a list of items ordered by their priority (lowest first).
type priorityQueue struct {
	items []*item
}

// item represents a single element in the priority queue.
type item struct {
	node     Node // The node this item represents
	priority int  // Priority value (lower = higher priority)
	index    int  // Position in the heap (maintained by heap operations)
}

// newPriorityQueue creates a new empty priority queue.
func newPriorityQueue() *priorityQueue {
	pq := &priorityQueue{
		items: make([]*item, 0),
	}
	heap.Init(pq)
	return pq
}

// newItem creates a new queue item.
func newItem(node Node, priority int) *item {
	return &item{
		node:     node,
		priority: priority,
		index:    -1,
	}
}

// Len returns the number of items in the queue.
func (pq *priorityQueue) Len() int {
	return len(pq.items)
}

// Less reports whether the item at index i should sort before the item at index j.
func (pq *priorityQueue) Less(i, j int) bool {
	return pq.items[i].priority < pq.items[j].priority
}

// Swap swaps the items at indices i and j.
func (pq *priorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
	pq.items[i].index = i
	pq.items[j].index = j
}

// Push adds a new item to the queue.
// The item should be of type *item.
func (pq *priorityQueue) Push(x interface{}) {
	item := x.(*item)
	item.index = len(pq.items)
	pq.items = append(pq.items, item)
}

// Pop removes and returns the minimum element (highest priority) from the queue.
func (pq *priorityQueue) Pop() interface{} {
	n := len(pq.items)
	item := pq.items[n-1]
	pq.items[n-1] = nil // Avoid memory leak
	item.index = -1     // Mark as removed
	pq.items = pq.items[:n-1]
	return item
}

// UpdatePriority modifies the priority of an item in the queue.
func (pq *priorityQueue) UpdatePriority(item *item, priority int) {
	item.priority = priority
	heap.Fix(pq, item.index)
}

// Peek returns the top item without removing it from the queue.
// Returns nil if the queue is empty.
func (pq *priorityQueue) Peek() *item {
	if len(pq.items) == 0 {
		return nil
	}
	return pq.items[0]
}

// Contains checks if a node exists in the queue and returns its item if found.
func (pq *priorityQueue) Contains(node Node) *item {
	for _, item := range pq.items {
		if item.node == node {
			return item
		}
	}
	return nil
}

// Push and Pop convenience methods that handle the heap operations

// SafePush adds an item to the queue and maintains the heap property
func (pq *priorityQueue) SafePush(item *item) {
	heap.Push(pq, item)
}

// SafePop removes and returns the highest priority item from the queue
func (pq *priorityQueue) SafePop() *item {
	if pq.Len() == 0 {
		return nil
	}
	return heap.Pop(pq).(*item)
}
