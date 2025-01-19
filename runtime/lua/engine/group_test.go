package engine

import (
	"context"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"sync"
	"testing"
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

// mockCVM implements the CVM interface for testing
type mockCVM struct{}

func (m *mockCVM) GetContext() context.Context                            { return context.Background() }
func (m *mockCVM) Start(string, ...lua.LValue) (<-chan TaskResult, error) { return nil, nil }
func (m *mockCVM) Step(...*Task) ([]*Task, error)                         { return nil, nil }
func (m *mockCVM) GetTasks() []*Task                                      { return nil }
func (m *mockCVM) GetTask(*lua.LState) (*Task, error)                     { return nil, nil }
func (m *mockCVM) Close()                                                 {}
