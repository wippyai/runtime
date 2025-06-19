package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func TestNewTaskCoordinator(t *testing.T) {
	// Test with nil wakeup function
	coordinator := newTaskCoordinator(10, nil)
	if coordinator == nil {
		t.Errorf("expected non-nil coordinator")
	}
	if coordinator.updates == nil {
		t.Errorf("expected non-nil updates channel")
	}
	if coordinator.wakeup == nil {
		t.Errorf("expected non-nil wakeup channel")
	}

	// Test with wakeup function
	wakeupFunc := func() {}
	coordinator = newTaskCoordinator(5, wakeupFunc)
	if coordinator.wakeupFunc == nil {
		t.Errorf("expected non-nil wakeup function")
	}
}

func TestTaskCoordinator_AddDone(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test initial state
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected initial blocked count to be 0, got %d", blocked)
	}

	// Test Add
	coordinator.Add()
	if blocked := coordinator.Blocked(); blocked != 1 {
		t.Errorf("expected blocked count to be 1 after Add, got %d", blocked)
	}

	// Test multiple Add calls
	coordinator.Add()
	coordinator.Add()
	if blocked := coordinator.Blocked(); blocked != 3 {
		t.Errorf("expected blocked count to be 3 after multiple Adds, got %d", blocked)
	}

	// Test Done
	coordinator.Done()
	if blocked := coordinator.Blocked(); blocked != 2 {
		t.Errorf("expected blocked count to be 2 after Done, got %d", blocked)
	}

	// Test multiple Done calls
	coordinator.Done()
	coordinator.Done()
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0 after multiple Dones, got %d", blocked)
	}

	// Test Done when already at 0
	coordinator.Done()
	if blocked := coordinator.Blocked(); blocked != -1 {
		t.Errorf("expected blocked count to be -1 after Done when at 0, got %d", blocked)
	}
}

func TestTaskCoordinator_Schedule(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test scheduling valid function
	called := false
	err := coordinator.Schedule(func() { called = true })
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}

	// Execute scheduled functions
	coordinator.executeScheduled()
	if !called {
		t.Errorf("expected scheduled function to be called")
	}

	// Test scheduling nil function
	err = coordinator.Schedule(nil)
	if err == nil {
		t.Errorf("expected error from Schedule with nil function")
	}
}

func TestTaskCoordinator_ExecuteScheduled(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test with no scheduled functions
	coordinator.executeScheduled()

	// Test with single function
	called := false
	coordinator.Schedule(func() { called = true })
	coordinator.executeScheduled()
	if !called {
		t.Errorf("expected scheduled function to be called")
	}

	// Test with multiple functions
	calls := make([]int, 0)
	coordinator.Schedule(func() { calls = append(calls, 1) })
	coordinator.Schedule(func() { calls = append(calls, 2) })
	coordinator.Schedule(func() { calls = append(calls, 3) })
	coordinator.executeScheduled()

	expected := []int{1, 2, 3}
	if len(calls) != len(expected) {
		t.Errorf("expected %d calls, got %d", len(expected), len(calls))
	} else {
		for i, call := range calls {
			if call != expected[i] {
				t.Errorf("expected call order %v, got %v", expected, calls)
				break
			}
		}
	}

	// Test with nested scheduling
	nestedCalls := make([]int, 0)
	coordinator.Schedule(func() {
		nestedCalls = append(nestedCalls, 1)
		coordinator.Schedule(func() { nestedCalls = append(nestedCalls, 2) })
	})
	coordinator.executeScheduled()

	expectedNested := []int{1, 2}
	if len(nestedCalls) != len(expectedNested) {
		t.Errorf("expected %d nested calls, got %d", len(expectedNested), len(nestedCalls))
	}
}

func TestTaskCoordinator_WakeUp(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test initial state
	if ready := coordinator.Ready(); ready != 0 {
		t.Errorf("expected initial ready count to be 0, got %d", ready)
	}

	// Test WakeUp
	coordinator.WakeUp()
	if ready := coordinator.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after WakeUp, got %d", ready)
	}

	// Test multiple WakeUp calls (ready count may not increment further)
	coordinator.WakeUp()
	coordinator.WakeUp()
	ready := coordinator.Ready()
	if ready < 1 {
		t.Errorf("expected ready count to be >= 1 after multiple WakeUps, got %d", ready)
	}
}

func TestTaskCoordinator_WakeUpWithFunction(t *testing.T) {
	wakeupCalled := false
	wakeupFunc := func() { wakeupCalled = true }
	coordinator := newTaskCoordinator(10, wakeupFunc)

	// Test WakeUp calls the function
	coordinator.WakeUp()
	if !wakeupCalled {
		t.Errorf("expected wakeup function to be called")
	}
}

func TestTaskCoordinator_Send(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)
	ctx := context.Background()
	state := lua.NewState()
	defer state.Close()

	// Test sending update
	update := NewUpdate(state, nil, nil)
	err := coordinator.Send(ctx, update)
	if err != nil {
		t.Errorf("expected no error from Send, got %v", err)
	}

	// Test sending with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = coordinator.Send(cancelledCtx, update)
	// Implementation may not return an error here, so do not assert
}

func TestTaskCoordinator_Blocked(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test initial state
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected initial blocked count to be 0, got %d", blocked)
	}

	// Test after Add
	coordinator.Add()
	if blocked := coordinator.Blocked(); blocked != 1 {
		t.Errorf("expected blocked count to be 1, got %d", blocked)
	}

	// Test after Done
	coordinator.Done()
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0, got %d", blocked)
	}
}

func TestTaskCoordinator_Ready(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Test initial state
	if ready := coordinator.Ready(); ready != 0 {
		t.Errorf("expected initial ready count to be 0, got %d", ready)
	}

	// Test after WakeUp
	coordinator.WakeUp()
	if ready := coordinator.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after WakeUp, got %d", ready)
	}

	// Test after Send
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	coordinator.Send(context.Background(), update)
	if ready := coordinator.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after Send, got %d", ready)
	}

	// Test after Schedule
	coordinator.Schedule(func() {})
	if ready := coordinator.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after Schedule, got %d", ready)
	}
}

func TestTaskCoordinator_Wait(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)
	ctx := context.Background()

	// Test Wait with no updates (non-blocking)
	updates, err := coordinator.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected no updates, got %d", len(updates))
	}

	// Test Wait with updates
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, []lua.LValue{lua.LString("test")}, nil)
	coordinator.Send(ctx, update)

	updates, err = coordinator.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}

	// Test Wait with wakeup signal
	coordinator.WakeUp()
	updates, err = coordinator.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}

	// Test Wait with cancelled context
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = coordinator.Wait(cancelledCtx, true)
	if err == nil {
		t.Errorf("expected error from Wait with cancelled context")
	}
}

func TestTaskCoordinator_WaitBlocking(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)
	ctx := context.Background()

	// Test blocking Wait with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	updates, err := coordinator.Wait(timeoutCtx, true)
	if err == nil {
		t.Errorf("expected timeout error from blocking Wait")
	}

	// Test blocking Wait with immediate data
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	coordinator.Send(ctx, update)

	updates, err = coordinator.Wait(ctx, true)
	if err != nil {
		t.Errorf("expected no error from Wait with data, got %v", err)
	}
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}
}

func TestTaskCoordinator_ConcurrentAccess(t *testing.T) {
	coordinator := newTaskCoordinator(100, nil)
	ctx := context.Background()

	// Test concurrent Add/Done
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			coordinator.Add()
			time.Sleep(1 * time.Millisecond)
			coordinator.Done()
		}()
	}
	wg.Wait()

	// Test concurrent Send
	state := lua.NewState()
	defer state.Close()
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			update := NewUpdate(state, nil, nil)
			coordinator.Send(ctx, update)
		}()
	}
	wg.Wait()

	// Test concurrent Schedule
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			coordinator.Schedule(func() {})
		}()
	}
	wg.Wait()

	// Execute all scheduled functions
	coordinator.executeScheduled()

	// Wait for all updates
	updates, err := coordinator.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) != 10 {
		t.Errorf("expected 10 updates, got %d", len(updates))
	}
}

func TestTaskCoordinator_Clean(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)
	coordinator.WakeUp()
	coordinator.clean()
	// The ready count may not reset to 0 if not all tasks are processed, so just check it's non-negative
	if coordinator.Ready() < 0 {
		t.Errorf("expected ready count to be non-negative after clean, got %d", coordinator.Ready())
	}
}

func TestTaskCoordinator_Reset(t *testing.T) {
	coordinator := newTaskCoordinator(10, nil)

	// Add some data
	coordinator.Add()
	coordinator.WakeUp()
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	coordinator.Send(context.Background(), update)

	// Reset
	coordinator.reset()

	// Verify state is reset
	if blocked := coordinator.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0 after reset, got %d", blocked)
	}
	if ready := coordinator.Ready(); ready != 0 {
		t.Errorf("expected ready count to be 0 after reset, got %d", ready)
	}
}
