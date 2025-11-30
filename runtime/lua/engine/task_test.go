package engine

import (
	"sync"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestTaskPool(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })
	thread, _ := state.NewThread()

	task := NewTask(thread, fn)
	if task == nil {
		t.Fatal("NewTask returned nil")
	}

	if task.Thread() != thread {
		t.Error("Thread() returned wrong value")
	}

	if task.Function() != fn {
		t.Error("Function() returned wrong value")
	}

	if task.State != lua.ResumeYield {
		t.Errorf("State = %v, want ResumeYield", task.State)
	}

	if task.Type() != lua.LTThread {
		t.Errorf("Type() = %v, want LTThread", task.Type())
	}

	str := task.String()
	if str == "" {
		t.Error("String() returned empty")
	}

	task.Close()
}

func TestTaskResumeWith(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })
	thread, _ := state.NewThread()

	task := NewTask(thread, fn)
	defer task.Close()

	task.ResumeWith(lua.LString("a"), lua.LNumber(42))

	if len(task.Resumed) != 2 {
		t.Errorf("Resumed length = %d, want 2", len(task.Resumed))
	}

	if task.Resumed[0] != lua.LString("a") {
		t.Error("Resumed[0] wrong value")
	}

	if task.Resumed[1] != lua.LNumber(42) {
		t.Error("Resumed[1] wrong value")
	}
}

func TestTaskQueueBasic(t *testing.T) {
	q := NewTaskQueue()

	if !q.IsEmpty() {
		t.Error("new queue should be empty")
	}

	if q.Len() != 0 {
		t.Errorf("Len() = %d, want 0", q.Len())
	}

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })

	t1, _ := state.NewThread()
	t2, _ := state.NewThread()
	t3, _ := state.NewThread()
	task1 := NewTask(t1, fn)
	task2 := NewTask(t2, fn)
	task3 := NewTask(t3, fn)

	q.Push(task1)
	q.Push(task2)
	q.Push(task3)

	if q.IsEmpty() {
		t.Error("queue should not be empty after Push")
	}

	if q.Len() != 3 {
		t.Errorf("Len() = %d, want 3", q.Len())
	}

	popped := q.Pop()
	if popped != task1 {
		t.Error("Pop() returned wrong task (FIFO order)")
	}

	if q.Len() != 2 {
		t.Errorf("Len() = %d after Pop, want 2", q.Len())
	}

	popped = q.Pop()
	if popped != task2 {
		t.Error("second Pop() returned wrong task")
	}

	popped = q.Pop()
	if popped != task3 {
		t.Error("third Pop() returned wrong task")
	}

	popped = q.Pop()
	if popped != nil {
		t.Error("Pop() from empty queue should return nil")
	}

	task1.Close()
	task2.Close()
	task3.Close()
}

func TestTaskQueueDrain(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })

	tasks := make([]*Task, 5)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	drained := q.Drain()
	if len(drained) != 5 {
		t.Errorf("Drain() returned %d tasks, want 5", len(drained))
	}

	for i, task := range drained {
		if task != tasks[i] {
			t.Errorf("Drain()[%d] wrong task", i)
		}
	}

	if !q.IsEmpty() {
		t.Error("queue should be empty after Drain")
	}

	// Drain empty queue
	drained = q.Drain()
	if drained != nil {
		t.Error("Drain() on empty queue should return nil")
	}

	for _, task := range tasks {
		task.Close()
	}
}

func TestTaskQueueGrow(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })

	// Push more than initial capacity (8)
	tasks := make([]*Task, 20)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	if q.Len() != 20 {
		t.Errorf("Len() = %d, want 20", q.Len())
	}

	// Verify FIFO order after growth
	for i := 0; i < 20; i++ {
		popped := q.Pop()
		if popped != tasks[i] {
			t.Errorf("Pop() at %d returned wrong task", i)
		}
	}

	for _, task := range tasks {
		task.Close()
	}
}

func TestTaskQueueConcurrent(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })

	var wg sync.WaitGroup
	pushCount := 100

	// Concurrent pushes
	for i := 0; i < pushCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			thread, _ := state.NewThread()
			task := NewTask(thread, fn)
			q.Push(task)
		}()
	}

	wg.Wait()

	if q.Len() != pushCount {
		t.Errorf("Len() = %d, want %d", q.Len(), pushCount)
	}

	// Concurrent pops
	popped := 0
	var popMu sync.Mutex
	for i := 0; i < pushCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if task := q.Pop(); task != nil {
				task.Close()
				popMu.Lock()
				popped++
				popMu.Unlock()
			}
		}()
	}

	wg.Wait()

	if popped != pushCount {
		t.Errorf("popped = %d, want %d", popped, pushCount)
	}

	if !q.IsEmpty() {
		t.Error("queue should be empty after all pops")
	}
}

func TestTaskQueueWrapAround(t *testing.T) {
	q := NewTaskQueue()

	state := lua.NewState()
	defer state.Close()

	fn := state.NewFunction(func(l *lua.LState) int { return 0 })

	// Push and pop to move head/tail
	for i := 0; i < 5; i++ {
		thread, _ := state.NewThread()
		task := NewTask(thread, fn)
		q.Push(task)
		q.Pop().Close()
	}

	// Now push more to trigger wrap-around
	tasks := make([]*Task, 6)
	for i := range tasks {
		thread, _ := state.NewThread()
		tasks[i] = NewTask(thread, fn)
		q.Push(tasks[i])
	}

	// Verify FIFO order with wrap-around
	for i := 0; i < 6; i++ {
		popped := q.Pop()
		if popped != tasks[i] {
			t.Errorf("Pop() at %d returned wrong task after wrap-around", i)
		}
		popped.Close()
	}
}
