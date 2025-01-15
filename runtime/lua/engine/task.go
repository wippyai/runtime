package engine

import (
	"container/list"
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

type Task struct {
	l      *lua.LState
	thread *lua.LState
	cancel context.CancelFunc
	fn     *lua.LFunction
	output chan TaskResult

	State      lua.ResumeState
	Yielded    []lua.LValue
	Resumed    []lua.LValue
	RaiseError error
}

func (t *Task) Thread() *lua.LState {
	return t.thread
}

func (t *Task) Type() lua.LValueType {
	return lua.LTThread
}

func (t *Task) String() string {
	return fmt.Sprintf("<coroutine %p> %+v", t.thread, t.Yielded)
}

type TaskResult struct {
	// Single value!
	Result []lua.LValue
	Error  error
}

type TaskQueue struct {
	active *list.List
}

func NewTaskQueue() *TaskQueue {
	return &TaskQueue{
		active: list.New(),
	}
}

func (q *TaskQueue) Push(task *Task) {
	q.active.PushBack(task)
}

func (q *TaskQueue) Pop() *Task {
	if q.active.Len() == 0 {
		return nil
	}
	e := q.active.Front()
	q.active.Remove(e)
	return e.Value.(*Task)
}

func (q *TaskQueue) Drain() []*Task {
	tasks := make([]*Task, 0, q.active.Len())
	for e := q.active.Front(); e != nil; e = e.Next() {
		t := e.Value.(*Task)
		if t != nil {
			tasks = append(tasks, t)
		}
	}
	q.active.Init()
	return tasks
}

func (q *TaskQueue) IsEmpty() bool {
	return q.active.Len() == 0
}

func (q *TaskQueue) Len() int {
	return q.active.Len()
}
