// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
)

// --- TaskQueue ---

func TestTaskQueue_NewEmpty(t *testing.T) {
	q := NewTaskQueue()
	assert.True(t, q.IsEmpty())
	assert.Equal(t, 0, q.Len())
}

func TestTaskQueue_PushPop(t *testing.T) {
	q := NewTaskQueue()
	task := &Task{}
	q.Push(task)

	assert.False(t, q.IsEmpty())
	assert.Equal(t, 1, q.Len())

	got := q.Pop()
	assert.Equal(t, task, got)
	assert.True(t, q.IsEmpty())
}

func TestTaskQueue_Pop_Empty(t *testing.T) {
	q := NewTaskQueue()
	assert.Nil(t, q.Pop())
}

func TestTaskQueue_FIFO(t *testing.T) {
	q := NewTaskQueue()
	t1 := &Task{State: 1}
	t2 := &Task{State: 2}
	t3 := &Task{State: 3}

	q.Push(t1)
	q.Push(t2)
	q.Push(t3)

	assert.Equal(t, t1, q.Pop())
	assert.Equal(t, t2, q.Pop())
	assert.Equal(t, t3, q.Pop())
	assert.Nil(t, q.Pop())
}

func TestTaskQueue_Grow(t *testing.T) {
	q := NewTaskQueue()
	tasks := make([]*Task, 20)

	for i := range tasks {
		tasks[i] = &Task{State: lua.ResumeState(i)}
		q.Push(tasks[i])
	}

	assert.Equal(t, 20, q.Len())

	for i := range tasks {
		got := q.Pop()
		require.NotNil(t, got)
		assert.Equal(t, lua.ResumeState(i), got.State)
	}
}

func TestTaskQueue_WrapAround(t *testing.T) {
	q := NewTaskQueue()
	// fill partially, pop some, then push more to test wrap-around
	for i := 0; i < 5; i++ {
		q.Push(&Task{State: lua.ResumeState(i)})
	}
	for i := 0; i < 3; i++ {
		got := q.Pop()
		assert.Equal(t, lua.ResumeState(i), got.State)
	}

	// head is now at index 3, push more to wrap around the ring buffer
	for i := 5; i < 12; i++ {
		q.Push(&Task{State: lua.ResumeState(i)})
	}

	// verify FIFO ordering still holds after wrap-around
	for i := 3; i < 12; i++ {
		got := q.Pop()
		require.NotNil(t, got, "expected task at index %d", i)
		assert.Equal(t, lua.ResumeState(i), got.State)
	}
	assert.True(t, q.IsEmpty())
}

func TestTaskQueue_Drain_Empty(t *testing.T) {
	q := NewTaskQueue()
	assert.Nil(t, q.Drain())
}

func TestTaskQueue_Drain(t *testing.T) {
	q := NewTaskQueue()
	t1 := &Task{State: 1}
	t2 := &Task{State: 2}
	q.Push(t1)
	q.Push(t2)

	drained := q.Drain()
	assert.Len(t, drained, 2)
	assert.Equal(t, t1, drained[0])
	assert.Equal(t, t2, drained[1])
	assert.True(t, q.IsEmpty())
}

func TestTaskQueue_Drain_ThenPush(t *testing.T) {
	q := NewTaskQueue()
	q.Push(&Task{State: 1})
	q.Push(&Task{State: 2})
	q.Drain()

	// push again after drain
	t3 := &Task{State: 3}
	q.Push(t3)
	assert.Equal(t, 1, q.Len())
	assert.Equal(t, t3, q.Pop())
}

// --- Task ---

func TestNewTask(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	fn, err := l.LoadString("return 1")
	require.NoError(t, err)

	thread, _ := l.NewThread()
	task := NewTask(thread, fn)
	defer task.Close()

	assert.Equal(t, thread, task.Thread())
	assert.Equal(t, fn, task.Function())
	assert.Equal(t, lua.ResumeYield, task.State)
	assert.Nil(t, task.Yielded)
	assert.Nil(t, task.Resumed)
}

func TestTask_Type(t *testing.T) {
	task := &Task{}
	assert.Equal(t, lua.LTThread, task.Type())
}

func TestTask_String(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	thread, _ := l.NewThread()
	task := &Task{thread: thread}

	s := task.String()
	assert.Contains(t, s, "<coroutine")
}

func TestTask_ResumeWith(t *testing.T) {
	task := &Task{
		resumeBuf: make([]lua.LValue, 0, 4),
	}

	task.ResumeWith(lua.LNumber(42), lua.LString("hello"))
	require.Len(t, task.Resumed, 2)
	assert.Equal(t, lua.LNumber(42), task.Resumed[0])
	assert.Equal(t, lua.LString("hello"), task.Resumed[1])
}

func TestTask_ResumeWith_Overwrites(t *testing.T) {
	task := &Task{
		resumeBuf: make([]lua.LValue, 0, 4),
	}

	task.ResumeWith(lua.LNumber(1))
	task.ResumeWith(lua.LNumber(2), lua.LNumber(3))
	assert.Len(t, task.Resumed, 2)
	assert.Equal(t, lua.LNumber(2), task.Resumed[0])
}

func TestTask_Close(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	thread, _ := l.NewThread()
	fn, _ := l.LoadString("return 1")
	task := NewTask(thread, fn)

	task.Close()
	assert.Nil(t, task.thread)
	assert.Nil(t, task.fn)
	assert.Nil(t, task.Yielded)
	assert.Nil(t, task.Resumed)
}

func TestTask_PoolReuse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	fn, _ := l.LoadString("return 1")

	// create and close a task to return to pool
	thread1, _ := l.NewThread()
	task1 := NewTask(thread1, fn)
	task1.Close()

	// next task may reuse the pooled object
	thread2, _ := l.NewThread()
	task2 := NewTask(thread2, fn)
	assert.Equal(t, thread2, task2.Thread())
	assert.Equal(t, fn, task2.Function())
	assert.Equal(t, lua.ResumeYield, task2.State)
	task2.Close()
}
