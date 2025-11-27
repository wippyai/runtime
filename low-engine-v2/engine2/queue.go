package engine2

import (
	"sync"
)

const defaultQueueCap = 8

// TaskQueue manages a queue of coroutine tasks waiting for execution.
// Uses a slice-based ring buffer to avoid allocations.
type TaskQueue struct {
	items []*Task
	head  int
	tail  int
	count int
	mu    sync.Mutex

	// reusable drain buffer
	drainBuf []*Task
}

// NewTaskQueue creates and initializes a new TaskQueue instance.
func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		items:    make([]*Task, defaultQueueCap),
		drainBuf: make([]*Task, 0, defaultQueueCap),
	}
}

// Push adds a task to the end of the queue.
func (q *TaskQueue) Push(task *Task) {
	q.mu.Lock()
	if q.count == len(q.items) {
		q.grow()
	}
	q.items[q.tail] = task
	q.tail = (q.tail + 1) % len(q.items)
	q.count++
	q.mu.Unlock()
}

// grow doubles the capacity of the ring buffer.
func (q *TaskQueue) grow() {
	newCap := len(q.items) * 2
	newItems := make([]*Task, newCap)

	// Copy items in order
	for i := 0; i < q.count; i++ {
		newItems[i] = q.items[(q.head+i)%len(q.items)]
	}
	q.items = newItems
	q.head = 0
	q.tail = q.count
}

// Pop removes and returns the first task in the queue.
// Returns nil if the queue is empty.
func (q *TaskQueue) Pop() *Task {
	q.mu.Lock()
	if q.count == 0 {
		q.mu.Unlock()
		return nil
	}
	task := q.items[q.head]
	q.items[q.head] = nil // clear reference
	q.head = (q.head + 1) % len(q.items)
	q.count--
	q.mu.Unlock()
	return task
}

// Drain removes and returns all tasks from the queue.
// Reuses internal buffer to avoid allocations.
func (q *TaskQueue) Drain() []*Task {
	q.mu.Lock()
	if q.count == 0 {
		q.mu.Unlock()
		return nil
	}

	// Reuse drain buffer, grow if needed
	if cap(q.drainBuf) < q.count {
		q.drainBuf = make([]*Task, 0, q.count*2)
	}
	q.drainBuf = q.drainBuf[:0]

	for i := 0; i < q.count; i++ {
		idx := (q.head + i) % len(q.items)
		q.drainBuf = append(q.drainBuf, q.items[idx])
		q.items[idx] = nil // clear reference
	}

	q.head = 0
	q.tail = 0
	q.count = 0
	q.mu.Unlock()

	return q.drainBuf
}

// IsEmpty returns true if the queue contains no tasks.
func (q *TaskQueue) IsEmpty() bool {
	q.mu.Lock()
	empty := q.count == 0
	q.mu.Unlock()
	return empty
}

// Len returns the number of tasks currently in the queue.
func (q *TaskQueue) Len() int {
	q.mu.Lock()
	n := q.count
	q.mu.Unlock()
	return n
}
