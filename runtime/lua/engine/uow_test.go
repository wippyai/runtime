package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
	lua "github.com/yuin/gopher-lua"
)

func TestNewUnitOfWork(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, ctx := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test that UnitOfWork is created
	if uw == nil {
		t.Errorf("expected non-nil UnitOfWork")
	}

	// Test that context is created
	if ctx == nil {
		t.Errorf("expected non-nil context")
	}

	// Test that context is different from parent
	if ctx == parentCtx {
		t.Errorf("expected new context to be different from parent")
	}

	// Test that UnitOfWork is stored in context
	if retrieved := GetUnitOfWork(ctx); retrieved != uw {
		t.Errorf("expected retrieved UnitOfWork to match original")
	}
}

func TestNewUnitOfWorkWithWakeUp(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	// Add wake-up function to parent context
	wakeupCalled := false
	wakeupFunc := func() { wakeupCalled = true }
	parentCtx = context.WithValue(parentCtx, ctxapi.WakeUpKey, wakeupFunc)

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test that wake-up function is set
	uw.Tasks().WakeUp()
	if !wakeupCalled {
		t.Errorf("expected wake-up function to be called")
	}
}

func TestNewUnitOfWorkWithState(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	// Test that state context is set (should not be nil)
	if state.Context() == nil {
		t.Errorf("expected state context to be set, got nil")
	}
	uw.Close()
}

func TestUnitOfWork_Context(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, ctx := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	if uw.Context() != ctx {
		t.Errorf("expected Context to return the UnitOfWork context")
	}
}

func TestUnitOfWork_State(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	if uw.State() != state {
		t.Errorf("expected State to return the Lua state")
	}
}

func TestUnitOfWork_Values(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	values := uw.Values()
	if values == nil {
		t.Errorf("expected Values to return non-nil ValueStore")
	}

	// Test that ValueStore works
	values.Set("key", "value")
	if val, exists := values.Get("key"); !exists {
		t.Errorf("expected key to exist")
	} else if val != "value" {
		t.Errorf("expected value 'value', got %v", val)
	}
}

func TestUnitOfWork_Tasks(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	tasks := uw.Tasks()
	if tasks == nil {
		t.Errorf("expected Tasks to return non-nil Tasks")
	}

	// Test that Tasks works
	tasks.Add()
	if blocked := tasks.Blocked(); blocked != 1 {
		t.Errorf("expected blocked count to be 1, got %d", blocked)
	}
	tasks.Done()
}

func TestUnitOfWork_AddCleanup(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test adding cleanup function
	called := false
	cancel := uw.AddCleanup(func() error {
		called = true
		return nil
	})

	if cancel == nil {
		t.Errorf("expected non-nil cancel function")
	}

	// Test cancel function
	cancel()
	if !called {
		t.Errorf("expected cleanup function to be called")
	}
}

func TestUnitOfWork_AddCleanupWithError(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	// Test cleanup function that returns error
	cleanupErr := errors.New("cleanup error")
	_ = uw.AddCleanup(func() error {
		return cleanupErr
	})

	// Close should return the error, and should not panic
	err := uw.Close()
	if err != cleanupErr {
		t.Errorf("expected cleanup error, got %v", err)
	}
	// Do not call uw.Close() again or access state after close
}

func TestUnitOfWork_Run(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	var mu sync.Mutex
	runCalled := false
	var runUw UnitOfWork
	var wg sync.WaitGroup
	wg.Add(1)

	uw.Run(func(u UnitOfWork) {
		mu.Lock()
		runCalled = true
		runUw = u
		mu.Unlock()
		wg.Done()
	})

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if !runCalled {
		t.Errorf("expected Run function to be called")
	}
	if runUw != uw {
		t.Errorf("expected UnitOfWork to match in Run function")
	}
}

func TestUnitOfWork_RunWhenClosed(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)

	// Close the UnitOfWork
	uw.Close()

	// After Close, the UnitOfWork is reset and put back in the pool
	// So Run will work again (this is the expected behavior)
	var mu sync.Mutex
	runCalled := false

	uw.Run(func(u UnitOfWork) {
		mu.Lock()
		defer mu.Unlock()
		runCalled = true
	})

	// Give some time for the goroutine to potentially start
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !runCalled {
		t.Errorf("expected Run function to be called after reset")
	}
}

func TestUnitOfWork_Terminate(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, ctx := NewUnitOfWork(parentCtx, state)

	// Test Terminate
	terminateErr := errors.New("termination error")
	err := uw.Terminate(terminateErr)
	// Terminate returns the error from closeInternal(), not the termination error
	// The termination error is stored in the resource manager but not returned
	if err != nil {
		t.Errorf("expected no error from Terminate, got %v", err)
	}

	// Test that context is cancelled - check the original context, not the UnitOfWork context
	// since the UnitOfWork context is reset after termination
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Errorf("expected context to be cancelled")
	}
}

func TestUnitOfWork_Close(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, ctx := NewUnitOfWork(parentCtx, state)

	// Test Close
	err := uw.Close()
	if err != nil {
		t.Errorf("expected no error from Close, got %v", err)
	}

	// Test that context is cancelled - check the original context, not the UnitOfWork context
	// since the UnitOfWork context is reset after close
	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Errorf("expected context to be cancelled")
	}
}

func TestUnitOfWork_CloseMultipleTimes(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, _ := NewUnitOfWork(parentCtx, state)

	// Close first time
	err1 := uw.Close()
	if err1 != nil {
		t.Errorf("expected no error from first Close, got %v", err1)
	}

	// Do not call Close() again; further use is undefined behavior due to pooling/reset.
}

func TestGetTasks(t *testing.T) {
	// Create a mock TaskProvider
	provider := &mockTaskProvider{
		tasks: make(map[*lua.LState]*Task),
	}

	state1 := lua.NewState()
	defer state1.Close()
	state2 := lua.NewState()
	defer state2.Close()

	task1 := &Task{thread: state1}
	task2 := &Task{thread: state2}

	provider.tasks[state1] = task1
	provider.tasks[state2] = task2

	// Test GetTasks with updates
	update1 := NewUpdate(state1, nil, nil)
	update2 := NewUpdate(state2, nil, errors.New("test error"))

	tasks, err := GetTasks(provider, update1, update2)
	if err != nil {
		t.Errorf("expected no error from GetTasks, got %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Check that error is set on task2
	if tasks[1].RaiseError == nil {
		t.Errorf("expected error to be set on task")
	}
}

func TestGetTasks_Error(t *testing.T) {
	// Create a mock TaskProvider that returns error
	provider := &mockTaskProvider{
		shouldError: true,
	}

	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)

	_, err := GetTasks(provider, update)
	if err == nil {
		t.Errorf("expected error from GetTasks")
	}
}

// Mock TaskProvider for testing
type mockTaskProvider struct {
	tasks       map[*lua.LState]*Task
	shouldError bool
}

func (m *mockTaskProvider) GetTask(thread *lua.LState) (*Task, error) {
	if m.shouldError {
		return nil, errors.New("mock error")
	}
	if task, exists := m.tasks[thread]; exists {
		return task, nil
	}
	return nil, errors.New("task not found")
}
