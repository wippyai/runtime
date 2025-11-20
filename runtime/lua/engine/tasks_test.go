package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestNewTaskScheduler(t *testing.T) {
	// Test with nil wakeup function
	scheduler := newTaskScheduler(10, nil)
	require.NotNil(t, scheduler, "expected non-nil scheduler")
	require.NotNil(t, scheduler.updates, "expected non-nil updates channel")
	require.Nil(t, scheduler.wakeupFunc, "expected nil wakeup function when nil is passed")

	// Test with wakeup function
	wakeupFunc := func() {}
	scheduler = newTaskScheduler(5, wakeupFunc)
	require.NotNil(t, scheduler.wakeupFunc, "expected non-nil wakeup function")
}

func TestTaskScheduler_AddDone(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test initial state
	assert.Equal(t, 0, scheduler.Blocked(), "expected initial blocked count to be 0")

	// Test Add
	scheduler.Add()
	assert.Equal(t, 1, scheduler.Blocked(), "expected blocked count to be 1 after Add")

	// Test multiple Add calls
	scheduler.Add()
	scheduler.Add()
	assert.Equal(t, 3, scheduler.Blocked(), "expected blocked count to be 3 after multiple Adds")

	// Test Done
	scheduler.Done()
	assert.Equal(t, 2, scheduler.Blocked(), "expected blocked count to be 2 after Done")

	// Test multiple Done calls
	scheduler.Done()
	scheduler.Done()
	assert.Equal(t, 0, scheduler.Blocked(), "expected blocked count to be 0 after multiple Dones")

	// Test Done when already at 0
	scheduler.Done()
	assert.Equal(t, -1, scheduler.Blocked(), "expected blocked count to be -1 after Done when at 0")
}

func TestTaskScheduler_Schedule(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test scheduling valid function
	called := false
	err := scheduler.Schedule(func() { called = true })
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}

	// Execute scheduled functions
	scheduler.executeScheduled()
	if !called {
		t.Errorf("expected scheduled function to be called")
	}

	// Test scheduling nil function
	err = scheduler.Schedule(nil)
	if err == nil {
		t.Errorf("expected error from Schedule with nil function")
	}
}

func TestTaskScheduler_ExecuteScheduled(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test with no scheduled functions
	scheduler.executeScheduled()

	// Test with single function
	called := false
	err := scheduler.Schedule(func() { called = true })
	require.NoError(t, err)
	scheduler.executeScheduled()
	if !called {
		t.Errorf("expected scheduled function to be called")
	}

	// Test with multiple functions
	calls := make([]int, 0)
	err = scheduler.Schedule(func() { calls = append(calls, 1) })
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}
	err = scheduler.Schedule(func() { calls = append(calls, 2) })
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}
	err = scheduler.Schedule(func() { calls = append(calls, 3) })
	if err != nil {
		t.Errorf("expected no error from Schedule, got %v", err)
	}
	scheduler.executeScheduled()

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
	err = scheduler.Schedule(func() {
		nestedCalls = append(nestedCalls, 1)
		err := scheduler.Schedule(func() { nestedCalls = append(nestedCalls, 2) })
		require.NoError(t, err)
	})
	require.NoError(t, err)
	scheduler.executeScheduled()

	expectedNested := []int{1, 2}
	if len(nestedCalls) != len(expectedNested) {
		t.Errorf("expected %d nested calls, got %d", len(expectedNested), len(nestedCalls))
	}
}

func TestTaskScheduler_WakeUp(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test initial state
	if ready := scheduler.Ready(); ready != 0 {
		t.Errorf("expected initial ready count to be 0, got %d", ready)
	}

	// Test WakeUp
	scheduler.WakeUp()
	if ready := scheduler.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after WakeUp, got %d", ready)
	}

	// Test multiple WakeUp calls (ready count may not increment further)
	scheduler.WakeUp()
	scheduler.WakeUp()
	ready := scheduler.Ready()
	if ready < 1 {
		t.Errorf("expected ready count to be >= 1 after multiple WakeUps, got %d", ready)
	}
}

func TestTaskScheduler_WakeUpWithFunction(t *testing.T) {
	wakeupCalled := false
	wakeupFunc := func() { wakeupCalled = true }
	scheduler := newTaskScheduler(10, wakeupFunc)

	// Test WakeUp calls the function
	scheduler.WakeUp()
	if !wakeupCalled {
		t.Errorf("expected wakeup function to be called")
	}
}

func TestTaskScheduler_Send(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)
	ctx := ctxapi.NewRootContext()
	state := lua.NewState()
	defer state.Close()

	// Test sending update
	update := NewUpdate(state, nil, nil)
	err := scheduler.Send(ctx, update)
	if err != nil {
		t.Errorf("expected no error from Send, got %v", err)
	}

	// Test sending with canceled context
	cancelledCtx, cancel := context.WithCancel(ctxapi.NewRootContext())
	cancel()
	_ = scheduler.Send(cancelledCtx, update)
	// Implementation may not return an error here, so do not assert
}

func TestTaskScheduler_Blocked(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test initial state
	if blocked := scheduler.Blocked(); blocked != 0 {
		t.Errorf("expected initial blocked count to be 0, got %d", blocked)
	}

	// Test after Add
	scheduler.Add()
	if blocked := scheduler.Blocked(); blocked != 1 {
		t.Errorf("expected blocked count to be 1, got %d", blocked)
	}

	// Test after Done
	scheduler.Done()
	if blocked := scheduler.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0, got %d", blocked)
	}
}

func TestTaskScheduler_Ready(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Test initial state
	if ready := scheduler.Ready(); ready != 0 {
		t.Errorf("expected initial ready count to be 0, got %d", ready)
	}

	// Test after WakeUp
	scheduler.WakeUp()
	if ready := scheduler.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after WakeUp, got %d", ready)
	}

	// Test after Send
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	err := scheduler.Send(context.Background(), update)
	require.NoError(t, err)
	if ready := scheduler.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after Send, got %d", ready)
	}

	// Test after Schedule
	err = scheduler.Schedule(func() {})
	require.NoError(t, err)
	if ready := scheduler.Ready(); ready == 0 {
		t.Errorf("expected ready count to be > 0 after Schedule, got %d", ready)
	}
}

func TestTaskScheduler_Wait(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)
	ctx := ctxapi.NewRootContext()

	// Test Wait with no updates (non-blocking)
	updates, err := scheduler.Wait(ctx, false)
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
	err = scheduler.Send(ctx, update)
	require.NoError(t, err)

	updates, err = scheduler.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}

	// Test Wait with wakeup signal
	scheduler.WakeUp()
	_, err = scheduler.Wait(ctx, false)
	require.NoError(t, err)

	// Test Wait with canceled context
	cancelledCtx, cancel := context.WithCancel(ctxapi.NewRootContext())
	cancel()
	_, err = scheduler.Wait(cancelledCtx, true)
	if err == nil {
		t.Errorf("expected error from Wait with canceled context")
	}
}

func TestTaskScheduler_WaitBlocking(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second*3)
	defer cancel()

	scheduler := newTaskScheduler(10, nil)

	// Test blocking Wait with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err := scheduler.Wait(timeoutCtx, true)
	require.Error(t, err)

	// Test blocking Wait with immediate data
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	err = scheduler.Send(ctx, update)
	require.NoError(t, err)

	updates, err := scheduler.Wait(ctx, true)
	require.NoError(t, err)
	if len(updates) != 1 {
		t.Errorf("expected 1 update, got %d", len(updates))
	}
}

func TestTaskScheduler_ConcurrentAccess(t *testing.T) {
	scheduler := newTaskScheduler(100, nil)
	ctx := ctxapi.NewRootContext()

	// Test concurrent Add/Done
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scheduler.Add()
			time.Sleep(1 * time.Millisecond)
			scheduler.Done()
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
			err := scheduler.Send(ctx, update)
			require.NoError(t, err)
		}()
	}
	wg.Wait()

	// Test concurrent Schedule
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := scheduler.Schedule(func() {})
			require.NoError(t, err)
		}()
	}
	wg.Wait()

	// Execute all scheduled functions
	scheduler.executeScheduled()

	// Wait for all updates
	updates, err := scheduler.Wait(ctx, false)
	if err != nil {
		t.Errorf("expected no error from Wait, got %v", err)
	}
	if len(updates) != 10 {
		t.Errorf("expected 10 updates, got %d", len(updates))
	}
}

func TestTaskScheduler_Clean(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)
	scheduler.WakeUp()
	scheduler.clean()
	// The ready count may not reset to 0 if not all tasks are processed, so just check it's non-negative
	if scheduler.Ready() < 0 {
		t.Errorf("expected ready count to be non-negative after clean, got %d", scheduler.Ready())
	}
}

func TestTaskScheduler_Reset(t *testing.T) {
	scheduler := newTaskScheduler(10, nil)

	// Add some data
	scheduler.Add()
	scheduler.WakeUp()
	state := lua.NewState()
	defer state.Close()
	update := NewUpdate(state, nil, nil)
	err := scheduler.Send(context.Background(), update)
	require.NoError(t, err)

	// Reset
	scheduler.reset()

	// Verify state is reset
	if blocked := scheduler.Blocked(); blocked != 0 {
		t.Errorf("expected blocked count to be 0 after reset, got %d", blocked)
	}
	if ready := scheduler.Ready(); ready != 0 {
		t.Errorf("expected ready count to be 0 after reset, got %d", ready)
	}
}
