package engine

import (
	"context"
	"errors"
	ctxapi "github.com/wippyai/runtime/api/context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestNewUpdate(t *testing.T) {
	state := lua.NewState()
	defer state.Close()

	// Test with nil result and error
	update := NewUpdate(state, nil, nil)
	if update.State != state {
		t.Errorf("expected state to match, got %v", update.State)
	}
	if update.Result != nil {
		t.Errorf("expected nil result, got %v", update.Result)
	}
	if update.Error != nil {
		t.Errorf("expected nil error, got %v", update.Error)
	}

	// Test with result and error
	result := []lua.LValue{lua.LString("test")}
	err := context.Canceled
	update = NewUpdate(state, result, err)
	if update.State != state {
		t.Errorf("expected state to match, got %v", update.State)
	}
	if len(update.Result) != 1 || update.Result[0].String() != "test" {
		t.Errorf("expected result to match, got %v", update.Result)
	}
	if !errors.Is(update.Error, err) {
		t.Errorf("expected error to match, got %v", update.Error)
	}
}

func TestCoroutineLeak_Error(t *testing.T) {
	leak := &CoroutineLeak{Count: 5}
	expected := "found orphaned coroutines: 5"
	if leak.Error() != expected {
		t.Errorf("expected error message %q, got %q", expected, leak.Error())
	}

	leak = &CoroutineLeak{Count: 0}
	expected = "found orphaned coroutines: 0"
	if leak.Error() != expected {
		t.Errorf("expected error message %q, got %q", expected, leak.Error())
	}
}

func TestDeadlockError_Error(t *testing.T) {
	// Test with multiple coroutines
	err := &DeadlockError{Count: 3}
	expected := "deadlock detected on 3 coroutines"
	if err.Error() != expected {
		t.Errorf("expected error message \"%s\", got \"%s\"", expected, err.Error())
	}

	// Test with single coroutine
	err = &DeadlockError{Count: 1}
	expected = "deadlock detected on 1 coroutines"
	if err.Error() != expected {
		t.Errorf("expected error message \"%s\", got \"%s\"", expected, err.Error())
	}
}

// Test interface implementations
func TestValueStoreInterface(t *testing.T) {
	store := newValueStore()

	// Test Get with non-existent key
	if _, exists := store.Get("nonexistent"); exists {
		t.Errorf("expected non-existent key to return false, got true")
	}

	// Test Set and Get
	store.Set("key1", "value1")
	if val, exists := store.Get("key1"); !exists {
		t.Errorf("expected key to exist after Set")
	} else if val != "value1" {
		t.Errorf("expected value 'value1', got %v", val)
	}

	// Test Delete
	store.Delete("key1")
	if _, exists := store.Get("key1"); exists {
		t.Errorf("expected key to not exist after Delete")
	}

	// Test GetOrStore
	if val, loaded := store.GetOrStore("key2", "value2"); loaded {
		t.Errorf("expected loaded to be false for new key")
	} else if val != "value2" {
		t.Errorf("expected value 'value2', got %v", val)
	}

	// Test GetOrStore with existing key
	if val, loaded := store.GetOrStore("key2", "different"); !loaded {
		t.Errorf("expected loaded to be true for existing key")
	} else if val != "value2" {
		t.Errorf("expected existing value 'value2', got %v", val)
	}

	// Test CompareAndSwap
	if !store.CompareAndSwap("key2", "value2", "newvalue") {
		t.Errorf("expected CompareAndSwap to succeed")
	}
	if val, _ := store.Get("key2"); val != "newvalue" {
		t.Errorf("expected value to be updated to 'newvalue', got %v", val)
	}

	// Test CompareAndSwap with wrong old value
	if store.CompareAndSwap("key2", "wrong", "another") {
		t.Errorf("expected CompareAndSwap to fail with wrong old value")
	}
	if val, _ := store.Get("key2"); val != "newvalue" {
		t.Errorf("expected value to remain unchanged, got %v", val)
	}
}

func TestTaskCoordinatorInterface(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test initial state
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected initial blocked count to be 0, got %d", blocked)
	}
	if ready := coordinator.Ready(); ready != 0 {
		t.Errorf("expected initial ready count to be 0, got %d", ready)
	}

	// Test Add and Done
	coordinator.Add()
	if blocked := coordinator.Blocked(); blocked != 1 {
		t.Errorf("expected blocked count to be 1 after Add, got %d", blocked)
	}

	coordinator.Done()
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0 after Done, got %d", blocked)
	}

	// Test Schedule
	err := coordinator.Schedule(func() {})
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}

	err = coordinator.Schedule(nil)
	if err == nil {
		t.Errorf("expected error from Schedule with nil function")
	}

	// Test WakeUp
	coordinator.WakeUp()
	if ready := coordinator.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after WakeUp, got %d", ready)
	}

	// Test Send
	ctx := ctxapi.NewRootContext()
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)

	err = coordinator.Send(ctx, update)
	if err != nil {
		t.Errorf("expected no error from Send, got %v", err)
	}

	// Test Wait
	updates, err := coordinator.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) == 0 {
		t.Errorf("expected updates from Wait, got none")
	}
}

func TestUnitOfWorkInterface(t *testing.T) {
	parentCtx := context.Background()
	state := lua.NewState()
	defer state.Close()

	uw, ctx := NewUnitOfWork(parentCtx, state)
	defer uw.Close()

	// Test Context
	if uw.Context() != ctx {
		t.Errorf("expected context to match")
	}

	// Test State
	if uw.State() != state {
		t.Errorf("expected state to match")
	}

	// Test Values
	values := uw.Values()
	if values == nil {
		t.Errorf("expected Values to return non-nil")
	}

	// Test Tasks
	tasks := uw.Tasks()
	if tasks == nil {
		t.Errorf("expected Tasks to return non-nil")
	}

	// Test AddCleanup
	cancel := uw.AddCleanup(func() error { return nil })
	if cancel == nil {
		t.Errorf("expected AddCleanup to return non-nil cancel function")
	}

	// Test Run
	var runCalled bool
	var wg sync.WaitGroup
	wg.Add(1)
	uw.Run(func(runUw UnitOfWork) {
		runCalled = true
		if runUw != uw {
			t.Errorf("expected UnitOfWork to match in Run function")
		}
		wg.Done()
	})

	// Give the goroutine time to run
	_, err := uw.Tasks().Wait(ctx, false)
	require.NoError(t, err)

	wg.Wait()
	if !runCalled {
		t.Errorf("expected Run function to be called")
	}
}

func TestGetUnitOfWork(t *testing.T) {
	// Test with nil context
	if uw := GetUnitOfWork(t.Context()); uw != nil {
		t.Errorf("expected nil UnitOfWork for nil context")
	}

	// Test with context without UnitOfWork
	ctx := ctxapi.NewRootContext()
	if uw := GetUnitOfWork(ctx); uw != nil {
		t.Errorf("expected nil UnitOfWork for context without UnitOfWork")
	}

	// Test with context containing UnitOfWork - need FrameContext
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	state := lua.NewState()
	defer state.Close()
	uw, ctx := NewUnitOfWork(ctx, state)
	defer uw.Close()

	if retrieved := GetUnitOfWork(ctx); retrieved != uw {
		t.Errorf("expected retrieved UnitOfWork to match original")
	}
}
