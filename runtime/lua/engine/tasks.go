package engine

import (
	"container/list"
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
)

//------------------------------------------------------------------------------
// Task Coordinator Implementation
//------------------------------------------------------------------------------

// taskCoordinator implements the Tasks interface for coroutine coordination
type taskCoordinator struct {
	updates    chan *Update  // Channel for task updates
	wakeup     chan struct{} // Signal channel for wake-up notifications
	wakeCount  atomic.Int32  // Counter for wake-up signals
	taskCount  atomic.Int32  // Counter for active threads
	updCount   atomic.Int32  // Counter for sent updates
	awaken     atomic.Bool   // Flag indicating if wake-up has been signaled
	wakeupFunc func()        // Function to call on wake-up

	// For scheduling arbitrary functions to execute during Wait
	schedMu   sync.Mutex // Mutex for scheduled functions
	scheduled *list.List // List of scheduled functions
}

// newTaskCoordinator creates a new task coordinator with specified buffer size
// and optional wakeup function
func newTaskCoordinator(bufferSize int, wakeupFunc func()) *taskCoordinator {
	return &taskCoordinator{
		updates:    make(chan *Update, bufferSize),
		wakeup:     make(chan struct{}, bufferSize),
		wakeupFunc: wakeupFunc,
		scheduled:  list.New(),
	}
}

// Add registers a new task and increments the task counter
func (t *taskCoordinator) Add() {
	t.taskCount.Add(1)
}

// Done signals that a task has completed and decrements the counter
func (t *taskCoordinator) Done() {
	t.taskCount.Add(^int32(0))
	t.WakeUp()
}

// Schedule adds a function to be executed during Wait
func (t *taskCoordinator) Schedule(fn func()) error {
	if fn == nil {
		return errors.New("cannot schedule nil function")
	}

	t.schedMu.Lock()
	t.scheduled.PushBack(fn)
	t.schedMu.Unlock()

	// Signal that there's work to do
	t.Add()
	t.WakeUp()
	return nil
}

// executeScheduled executes any scheduled functions including ones created by scheduled functions
func (t *taskCoordinator) executeScheduled() {
	for {
		t.schedMu.Lock()
		// If there are no functions, return quickly
		if t.scheduled.Len() == 0 {
			t.schedMu.Unlock()
			return
		}

		// Take the current list and replace with a new one
		funcs := t.scheduled
		t.scheduled = list.New()
		t.schedMu.Unlock()

		// Execute all scheduled functions
		for e := funcs.Front(); e != nil; e = e.Next() {
			if fn, ok := e.Value.(func()); ok && fn != nil {
				fn()
				t.Done()
			}
		}
	}
}

// WakeUp signals that threads may be ready to process
// This is thread-safe and can be called from any goroutine
func (t *taskCoordinator) WakeUp() {
	if t.awaken.CompareAndSwap(false, true) {
		if t.wakeupFunc != nil {
			t.wakeupFunc()
		}

		t.wakeCount.Add(1)
		select {
		case t.wakeup <- struct{}{}:
		default:
		}
	}
}

// Send pushes a result to the task channel and signals wake up
// This is thread-safe and can be called from any goroutine
func (t *taskCoordinator) Send(ctx context.Context, update *Update) error {
	t.updCount.Add(1)
	select {
	case t.updates <- update:
		t.WakeUp()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Count returns the current number of active threads
func (t *taskCoordinator) Count() int {
	return int(t.taskCount.Load())
}

// Awaken checks if the task group has been awakened
func (t *taskCoordinator) Awaken() bool {

	log.Printf(
		"CHECK AWAKEN %v %v %v %v",
		t.awaken.Load(),
		t.taskCount.Load(),
		t.updCount.Load(),
		t.scheduled.Len(),
	)

	t.schedMu.Lock()
	if t.scheduled.Len() != 0 {
		t.schedMu.Unlock() // we have something scheduled
		return true
	}
	t.schedMu.Unlock()

	return t.awaken.Load() // || t.updCount.Load() > 0
}

// Wait waits for updates or wake-up signals
// If block is true, it will wait for at least one result or wake-up signal
func (t *taskCoordinator) Wait(ctx context.Context, block bool) ([]*Update, error) {
	defer t.awaken.Store(false)

	updates := make([]*Update, 0)

	// Execute any pending scheduled functions first
	t.executeScheduled()

	// Process available updates or continue if task count is zero
	for t.taskCount.Load() > 0 {
		if block {
			select {
			case upd := <-t.updates:
				t.updCount.Add(^int32(0))
				if upd != nil {
					updates = append(updates, upd)
				}
				block = false
				continue

			case <-t.wakeup:
				t.wakeCount.Add(^int32(0))
				block = false
				continue

			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		// Non-blocking check for more updates or threads
		select {
		case upd := <-t.updates:
			t.updCount.Add(^int32(0))
			if upd != nil {
				updates = append(updates, upd)
			}

		case <-t.wakeup:
			t.wakeCount.Add(^int32(0))
		default:
			return updates, nil
		}
	}

	return updates, nil
}

// clean resets the task coordinator to its initial state
func (t *taskCoordinator) clean() {
	if t.taskCount.Load() == 0 {
		return
	}

	t.taskCount.Store(0)
	t.wakeCount.Store(0)

	// Clean up scheduled functions
	t.schedMu.Lock()
	t.scheduled.Init() // Reinitialize the list
	t.schedMu.Unlock()

	// Drain channels
	for {
		select {
		case <-t.updates:
			// Drain updates channel
		case <-t.wakeup:
			// Drain wakeup channel
		default:
			// Both channels empty, exit loop
			return
		}
	}
}

// Interface implementation verification
var _ Tasks = (*taskCoordinator)(nil)
