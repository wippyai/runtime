package engine

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestTaskGroup(t *testing.T) {
	t.Run("basic operations", func(t *testing.T) {
		group := NewTaskGroup(10)
		assert.NotNil(t, group)
		assert.Equal(t, 0, group.GetTaskCount())
	})

	t.Run("context attachment", func(t *testing.T) {
		group := NewTaskGroup(10)
		ctx := context.Background()

		ctxWithGroup := WithTaskGroup(ctx, group)
		retrievedGroup := GetTaskGroup(ctxWithGroup)
		assert.Equal(t, group, retrievedGroup)

		emptyGroup := GetTaskGroup(ctx)
		assert.Nil(t, emptyGroup)
	})

	t.Run("task tracking", func(t *testing.T) {
		group := NewTaskGroup(10)
		L := lua.NewState()
		defer L.Close()

		group.Add(L)
		assert.Equal(t, 1, group.GetTaskCount())
		assert.True(t, group.HasState(L))

		group.Remove(L)
		assert.Equal(t, 0, group.GetTaskCount())
		assert.False(t, group.HasState(L))
	})

	t.Run("multiple states", func(t *testing.T) {
		group := NewTaskGroup(10)
		L1 := lua.NewState()
		L2 := lua.NewState()
		defer L1.Close()
		defer L2.Close()

		group.Add(L1)
		group.Add(L2)
		assert.Equal(t, 2, group.GetTaskCount())

		states := group.GetActiveStates()
		assert.Equal(t, 2, len(states))
		assert.Contains(t, states, L1)
		assert.Contains(t, states, L2)
	})

	t.Run("result sending with context", func(t *testing.T) {
		// Create a TaskGroup with small buffer
		group := NewTaskGroup(2)
		result := TaskResult{State: lua.NewState(), Result: []lua.LValue{lua.LString("test")}}

		// Test 1: Successful send with active context
		err := group.Send(context.Background(), result)
		assert.NoError(t, err)

		// Fill up the channel buffer
		err = group.Send(context.Background(), result)
		assert.NoError(t, err)

		// Test 2: Send with cancelled context
		cancelCtx, cancel := context.WithCancel(context.Background())

		var sendErr error
		var wg sync.WaitGroup
		wg.Add(1)

		// Start goroutine that will try to send to full channel
		go func() {
			defer wg.Done()
			sendErr = group.Send(cancelCtx, result)
		}()

		// Give goroutine time to start blocking send
		cancel()
		wg.Wait()

		assert.Error(t, sendErr)
		assert.ErrorIs(t, sendErr, context.Canceled)
	})

	t.Run("wakeup mechanism", func(t *testing.T) {
		group := NewTaskGroup(10)

		// Test wakeup is non-blocking when buffer is full
		for i := 0; i < cap(group.wakeup)*2; i++ {
			group.WakeUp()
		}

		// Verify at least one wakeup signal is available
		select {
		case <-group.wakeup:
			// Success - received wakeup signal
		default:
			t.Error("no wakeup signal available")
		}
	})

	t.Run("cleanup", func(t *testing.T) {
		group := NewTaskGroup(10)
		L := lua.NewState()
		defer L.Close()

		group.Add(L)
		result := TaskResult{State: L, Result: []lua.LValue{lua.LString("test")}}
		_ = group.Send(context.Background(), result)

		group.clean()
		assert.Equal(t, 0, group.GetTaskCount())
		assert.Empty(t, group.states)

		// Verify results channel is drained
		select {
		case <-group.results:
			t.Error("results channel should be empty after cleanup")
		default:
			// Success - channel is empty
		}
	})

	t.Run("wait behavior", func(t *testing.T) {
		group := NewTaskGroup(10)
		L := lua.NewState()
		defer L.Close()

		// Create mock CVM
		mockCVM := &mockCVM{}

		// Test 1: Non-blocking wait with no tasks
		tasks, err := group.Wait(context.Background(), mockCVM, false)
		assert.NoError(t, err)
		assert.Empty(t, tasks)

		// Test 2: Wait with context cancellation
		ctx, cancel := context.WithCancel(context.Background())

		// Add a state to force Wait to block
		group.Add(L)

		var waitErr error
		var wg sync.WaitGroup
		wg.Add(1)

		// Start waiting in a goroutine
		go func() {
			defer wg.Done()
			_, waitErr = group.Wait(ctx, mockCVM, true)
		}()

		// Give the goroutine time to enter Wait
		cancel()
		wg.Wait()

		assert.Error(t, waitErr)
		assert.ErrorIs(t, waitErr, context.Canceled)
	})
}

func TestTaskGroupProcessing(t *testing.T) {
	t.Run("process result handling", func(t *testing.T) {
		group := NewTaskGroup(10)
		L := lua.NewState()
		defer L.Close()

		mockTask := &Task{
			thread: L,
			// Initialize with nil Resumed to match expected state
			Resumed: nil,
		}

		mockCVM := &mockCVMWithTasks{
			tasks: map[*lua.LState]*Task{
				L: mockTask,
			},
		}

		// Test successful result processing
		result := TaskResult{
			State:  L,
			Result: []lua.LValue{lua.LString("success")},
		}

		task, err := group.processResult(mockCVM, result)
		assert.NoError(t, err)
		assert.NotNil(t, task)
		assert.Equal(t, result.Result, task.Resumed)
		assert.Nil(t, task.RaiseError)

		// Test error result processing
		testErr := fmt.Errorf("test error")
		errorResult := TaskResult{
			State: L,
			Error: testErr,
		}

		task, err = group.processResult(mockCVM, errorResult)
		assert.NoError(t, err)
		assert.NotNil(t, task)
		assert.Equal(t, testErr, task.RaiseError)

		// Test CVM error
		mockCVM.shouldError = true
		task, err = group.processResult(mockCVM, result)
		assert.Error(t, err)
		assert.Nil(t, task)
	})

	t.Run("wait with result processing", func(t *testing.T) {
		group := NewTaskGroup(10)
		L1 := lua.NewState()
		L2 := lua.NewState()
		defer L1.Close()
		defer L2.Close()

		// Create tasks for our test states with proper initialization
		task1 := &Task{
			thread:  L1,
			Resumed: nil,
		}
		task2 := &Task{
			thread:  L2,
			Resumed: nil,
		}

		mockCVM := &mockCVMWithTasks{
			tasks: map[*lua.LState]*Task{
				L1: task1,
				L2: task2,
			},
		}

		// Add both states to the group
		group.Add(L1)
		group.Add(L2)

		// Create a WaitGroup to coordinate our goroutines
		var wg sync.WaitGroup
		ctx := context.Background()

		// Start a goroutine to send results
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Send first result immediately
			result1 := TaskResult{
				State:  L1,
				Result: []lua.LValue{lua.LString("result1")},
			}
			_ = group.Send(ctx, result1)

			// Send second result immediately after
			result2 := TaskResult{
				State:  L2,
				Result: []lua.LValue{lua.LString("result2")},
			}
			_ = group.Send(ctx, result2)
		}()

		// Start waiting for results - use a timeout context
		timeoutCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()

		tasks, err := group.Wait(timeoutCtx, mockCVM, true)
		wg.Wait()

		assert.NoError(t, err)
		assert.Len(t, tasks, 2)

		// Sort tasks by thread pointer for consistent comparison
		sortedTasks := make([]*Task, len(tasks))
		copy(sortedTasks, tasks)
		sort.Slice(sortedTasks, func(i, j int) bool {
			return fmt.Sprintf("%p", sortedTasks[i].thread) < fmt.Sprintf("%p", sortedTasks[j].thread)
		})

		// Verify task results in order
		assert.Equal(t, []lua.LValue{lua.LString("result1")}, sortedTasks[0].Resumed)
		assert.Equal(t, []lua.LValue{lua.LString("result2")}, sortedTasks[1].Resumed)
	})

	t.Run("wait with wakeup interruption", func(t *testing.T) {
		group := NewTaskGroup(10)
		L := lua.NewState()
		defer L.Close()

		group.Add(L)

		var wg sync.WaitGroup
		ctx := context.Background()

		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(50 * time.Millisecond)
			group.WakeUp()
		}()

		tasks, err := group.Wait(ctx, &mockCVM{}, true)
		wg.Wait()

		assert.NoError(t, err)
		assert.Empty(t, tasks)
		assert.Equal(t, int32(0), group.wakeCount)
	})
}

// mockCVM implements the CVM interface for testing
type mockCVM struct{}

func (m *mockCVM) Context() context.Context                               { return context.Background() }
func (m *mockCVM) Start(string, ...lua.LValue) (<-chan TaskResult, error) { return nil, nil }
func (m *mockCVM) Step(...*Task) ([]*Task, error)                         { return nil, nil }
func (m *mockCVM) GetTasks() []*Task                                      { return nil }
func (m *mockCVM) GetTask(*lua.LState) (*Task, error)                     { return nil, nil }
func (m *mockCVM) State() *lua.LState                                     { return nil }
func (m *mockCVM) Close()                                                 {}

// mockCVMWithTasks implements CVM interface with configurable task responses
type mockCVMWithTasks struct {
	tasks       map[*lua.LState]*Task
	shouldError bool
}

func (m *mockCVMWithTasks) Context() context.Context {
	return context.Background()
}

func (m *mockCVMWithTasks) Start(string, ...lua.LValue) (<-chan TaskResult, error) {
	return nil, nil
}

func (m *mockCVMWithTasks) Step(...*Task) ([]*Task, error) {
	return nil, nil
}

func (m *mockCVMWithTasks) GetTasks() []*Task {
	return nil
}

func (m *mockCVMWithTasks) GetTask(state *lua.LState) (*Task, error) {
	if m.shouldError {
		return nil, fmt.Errorf("mock CVM error")
	}
	if task, ok := m.tasks[state]; ok {
		return task, nil
	}
	return nil, fmt.Errorf("task not found")
}

func (m *mockCVMWithTasks) State() *lua.LState {
	return nil
}

func (m *mockCVMWithTasks) Close() {}
