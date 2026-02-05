package engine

import (
	"fmt"
	"sync"

	lua "github.com/wippyai/go-lua"
)

// Task pool for reuse
var taskPool = sync.Pool{
	New: func() any {
		return &Task{
			retBuf:    make([]lua.LValue, 0, 8),
			resumeBuf: make([]lua.LValue, 0, 4),
		}
	},
}

const defaultQueueCap = 8

// TaskQueue manages a queue of coroutine tasks waiting for execution.
// Uses a slice-based ring buffer to avoid allocations.
// Note: Lock-free - single-threaded process model guarantees no concurrent access.
type TaskQueue struct {
	items    []*Task
	drainBuf []*Task
	head     int
	tail     int
	count    int
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
	if q.count == len(q.items) {
		q.grow()
	}
	q.items[q.tail] = task
	q.tail = (q.tail + 1) % len(q.items)
	q.count++
}

// grow doubles the capacity of the ring buffer.
func (q *TaskQueue) grow() {
	newCap := len(q.items) * 2
	newItems := make([]*Task, newCap)

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
	if q.count == 0 {
		return nil
	}
	task := q.items[q.head]
	q.items[q.head] = nil
	q.head = (q.head + 1) % len(q.items)
	q.count--
	return task
}

// Drain removes and returns all tasks from the queue.
// Reuses internal buffer to avoid allocations.
func (q *TaskQueue) Drain() []*Task {
	if q.count == 0 {
		return nil
	}

	if cap(q.drainBuf) < q.count {
		q.drainBuf = make([]*Task, 0, q.count*2)
	}
	q.drainBuf = q.drainBuf[:0]

	for i := 0; i < q.count; i++ {
		idx := (q.head + i) % len(q.items)
		q.drainBuf = append(q.drainBuf, q.items[idx])
		q.items[idx] = nil
	}

	q.head = 0
	q.tail = 0
	q.count = 0

	return q.drainBuf
}

// IsEmpty returns true if the queue contains no tasks.
// Note: Single-threaded process model - count check is safe without lock.
func (q *TaskQueue) IsEmpty() bool {
	return q.count == 0
}

// Len returns the number of tasks currently in the queue.
// Note: Single-threaded process model - count check is safe without lock.
func (q *TaskQueue) Len() int {
	return q.count
}

// Task represents a coroutine execution unit in the Lua VM.
type Task struct {
	thread    *lua.LState
	fn        *lua.LFunction
	Yielded   []lua.LValue
	Resumed   []lua.LValue
	retBuf    []lua.LValue
	resumeBuf []lua.LValue
	State     lua.ResumeState
}

// NewTask creates a task from an existing thread and function.
func NewTask(thread *lua.LState, fn *lua.LFunction) *Task {
	t := taskPool.Get().(*Task)
	t.thread = thread
	t.fn = fn
	t.State = lua.ResumeYield
	t.Yielded = nil
	t.Resumed = nil
	t.retBuf = t.retBuf[:0]
	return t
}

// Thread returns the Lua state associated with this task's coroutine.
func (t *Task) Thread() *lua.LState {
	return t.thread
}

// Function returns the Lua function being executed.
func (t *Task) Function() *lua.LFunction {
	return t.fn
}

// Type returns the Lua type of this task (LTThread).
func (t *Task) Type() lua.LValueType {
	return lua.LTThread
}

func (t *Task) String() string {
	return fmt.Sprintf("<coroutine %p>", t.thread)
}

// ResumeWith sets the Resumed slice using the internal buffer.
func (t *Task) ResumeWith(values ...lua.LValue) {
	t.resumeBuf = t.resumeBuf[:0]
	t.resumeBuf = append(t.resumeBuf, values...)
	t.Resumed = t.resumeBuf
}

// Close releases the task's resources and returns it to the pool.
func (t *Task) Close() {
	if t.thread != nil {
		t.thread.Close()
		t.thread = nil
	}
	t.fn = nil
	t.Yielded = nil
	t.Resumed = nil
	t.State = 0
	taskPool.Put(t)
}
