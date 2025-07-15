package engine

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ------------------------------------------------------------------------------
// Task Coordinator Implementation
// ------------------------------------------------------------------------------

// taskCoordinator implements the Tasks interface for coroutine coordination
type taskCoordinator struct {
	updates   chan *Update  // Channel for task updates
	wakeCh    chan struct{} // Channel for context-aware wake-up signals
	taskCount atomic.Int32  // Counter for external activities, usually counting blocked channels
	updCount  atomic.Int32  // Counter for sent updates and internal updates
	wakeCount atomic.Int32  // Counter for wake-up signals
	awaken    atomic.Bool   // Flag indicating if wake-up has been signaled

	wmu        sync.Mutex // Mutex for scheduled functions
	wakeupFunc func()     // Function to call on wake-up

	// For scheduling arbitrary functions to execute during Wait
	smu             sync.Mutex // Mutex for scheduled functions
	scheduled       *list.List // Active list where Schedule() adds functions
	scheduledBackup *list.List // Backup list for swapping (always empty when swapped to)
	undelivered     atomic.Bool
}

// newTaskCoordinator creates a new task coordinator with specified buffer size
// and optional wakeup function
func newTaskCoordinator(bufferSize int, wakeupFunc func()) *taskCoordinator {
	return &taskCoordinator{
		updates:         make(chan *Update, bufferSize),
		wakeCh:          make(chan struct{}, 1),
		wakeupFunc:      wakeupFunc,
		scheduled:       list.New(),
		scheduledBackup: list.New(),
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

	t.smu.Lock()
	t.scheduled.PushBack(fn)
	t.undelivered.Store(true)
	t.smu.Unlock()

	t.WakeUp()
	return nil
}

// executeScheduled executes any scheduled functions including ones created by scheduled functions
func (t *taskCoordinator) executeScheduled() {
	t.smu.Lock()
	if t.scheduled.Len() == 0 && t.undelivered.CompareAndSwap(true, false) {
		t.smu.Unlock()
		return
	}
	t.smu.Unlock()

	for {
		t.smu.Lock()
		// If there are no functions, return quickly
		if t.scheduled.Len() == 0 {
			t.smu.Unlock()

			// we are done with a queue, but we have to ensure to be
			// back to this function to propagate whole cycle
			return
		}

		// Swap lists instead of creating new one - avoid 1.85GB allocation
		t.scheduled, t.scheduledBackup = t.scheduledBackup, t.scheduled // scheduledBackup is always empty when we swap to it
		funcs := t.scheduledBackup                                      // Will be cleared after execution
		t.smu.Unlock()

		// Execute all scheduled functions
		for e := funcs.Front(); e != nil; e = e.Next() {
			if fn, ok := e.Value.(func()); ok && fn != nil {
				fn()
			}
		}

		// Clear the backup list for next iteration (maintains invariant: scheduledBackup is always empty)
		t.scheduledBackup.Init()
	}
}

// WakeUp signals that threads may be ready to process
// This is thread-safe and can be called from any goroutine
func (t *taskCoordinator) WakeUp() {
	t.wakeCount.Add(1)
	if t.awaken.CompareAndSwap(false, true) {
		// Signal the wake channel for context-aware waiting
		select {
		case t.wakeCh <- struct{}{}:
		default:
			// Channel is full, which means someone is already waiting
		}
	}

	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.wakeupFunc != nil {
		t.wakeupFunc()
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
		t.updCount.Add(^int32(0))
		return ctx.Err()
	}
}

// Blocked returns the current number of tasks that are currently running externally.
func (t *taskCoordinator) Blocked() int {
	return int(t.taskCount.Load())
}

// Ready returns the number of tasks that are currently ready to be processed
func (t *taskCoordinator) Ready() int {
	ready := int(t.updCount.Load()) + int(t.wakeCount.Load())
	if t.undelivered.Load() {
		// this flag is true until executeScheduled is called with empty list
		ready++
	}

	return ready
}

// Wait waits for updates or wake-up signals
// If block is true, it will wait for at least one result or wake-up signal
func (t *taskCoordinator) Wait(ctx context.Context, block bool) ([]*Update, error) {
	updates := make([]*Update, 0)

	// Execute any pending scheduled functions first
	t.executeScheduled()

	// Process available updates or continue if task count is zero
	for t.Ready() > 0 || block {
		if block {
			select {
			case upd := <-t.updates:
				t.updCount.Add(^int32(0))
				if upd != nil {
					updates = append(updates, upd)
				}
				block = false
				continue

			case <-ctx.Done():
				return nil, ctx.Err()

			default:
				// No updates available, wait for signal with context awareness
				// Use the wakeCh channel for context-aware waiting
				select {
				case <-t.wakeCh:
					// Consume the wake-up signal
					t.awaken.Store(false)
					block = false
					continue

				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		// Non-blocking check for more updates
		select {
		case upd := <-t.updates:
			t.updCount.Add(^int32(0))
			if upd != nil {
				updates = append(updates, upd)
			}
		default:
			// Consume any pending wake-up signals
			if t.wakeCount.Load() > 0 {
				t.wakeCount.Store(0)
			}
			return updates, nil
		}
	}

	return updates, nil
}

// clean resets the task coordinator to its initial state
func (t *taskCoordinator) clean() {
	done := false
	for !done {
		select {
		case <-t.updates:
		default:
			done = true
		}
	}

	if t.taskCount.Load() == 0 {
		return
	}

	t.taskCount.Store(0)

	// Clean up scheduled functions
	t.smu.Lock()
	t.scheduled.Init()       // Reinitialize the active list
	t.scheduledBackup.Init() // Reinitialize the backup list
	t.smu.Unlock()
}

func (t *taskCoordinator) reset() {
	t.wmu.Lock()
	t.wakeupFunc = nil
	t.wmu.Unlock()

	t.clean()

	// Reset all atomic counters
	t.taskCount.Store(0)
	t.updCount.Store(0)
	t.wakeCount.Store(0)
	t.awaken.Store(false)
	t.undelivered.Store(false)

	// Reset scheduled functions lists
	t.smu.Lock()
	t.scheduled.Init()
	t.scheduledBackup.Init()
	t.smu.Unlock()

	// Drain wake channel
	select {
	case <-t.wakeCh:
	default:
	}
}

// Interface implementation verification
var _ Tasks = (*taskCoordinator)(nil)
