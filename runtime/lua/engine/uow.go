package engine

import (
	"context"
	lua "github.com/yuin/gopher-lua"
	"sync"
	"sync/atomic"
	"time"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// unitOfWork implements the UnitOfWork interface
type unitOfWork struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	state  *lua.LState // Added to satisfy StateProvider interface

	closed       atomic.Bool   // Ensures close happens exactly once
	closeOnce    sync.Once     // Additional protection for close operations
	closeTimeout time.Duration // Timeout for graceful shutdown

	valueStore      *valueStore
	resourceManager *resourceManager
	tasks           *taskCoordinator
}

// NewUnitOfWork creates a new UnitOfWork instance
func NewUnitOfWork(parentCtx context.Context, state *lua.LState) (UnitOfWork, context.Context) {
	return NewUnitOfWorkWithTimeout(parentCtx, state, 5*time.Second)
}

// NewUnitOfWorkWithTimeout creates a new UnitOfWork instance with specified close closeTimeout
func NewUnitOfWorkWithTimeout(parentCtx context.Context, state *lua.LState, timeout time.Duration) (UnitOfWork, context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)

	uow := &unitOfWork{
		ctx:             ctx,
		cancel:          cancel,
		state:           state,
		closeTimeout:    timeout,
		valueStore:      newValueStore(),
		resourceManager: newResourceManager(),
		tasks:           newTaskCoordinator(256, nil), // Reasonable buffer size
	}

	// Check for wake-up function in context
	if awake := parentCtx.Value(ctxapi.WakeUpKey); awake != nil {
		if fn, ok := awake.(func()); ok {
			uow.tasks.wakeupFunc = fn
		}
	}

	// Add cleanup for state context
	if state != nil {
		state.SetContext(ctx)
		uow.AddCleanup(func() error {
			if state.Context() != nil {
				state.SetContext(nil)
			}
			return nil
		})
	}

	// Store in context
	ctx = context.WithValue(ctx, unitOfWorkKey, uow)

	return uow, ctx
}

// GetUnitOfWork retrieves the UnitOfWork from a context
func GetUnitOfWork(ctx context.Context) UnitOfWork {
	if ctx == nil {
		return nil
	}

	if uw, ok := ctx.Value(unitOfWorkKey).(UnitOfWork); ok {
		return uw
	}

	return nil
}

// DetachUnitOfWork removes any parent UnitOfWork relationship
// without stopping the existing UnitOfWork
func DetachUnitOfWork(ctx context.Context) context.Context {
	if _, ok := ctx.Value(ctxapi.WakeUpKey).(func()); ok {
		ctx = context.WithValue(ctx, ctxapi.WakeUpKey, nil)
	}

	if GetUnitOfWork(ctx) != nil {
		ctx = context.WithValue(ctx, unitOfWorkKey, nil)
	}

	return ctx
}

// Context returns the managed context
func (u *unitOfWork) Context() context.Context {
	return u.ctx
}

// State implements the StateProvider interface
func (u *unitOfWork) State() *lua.LState {
	return u.state
}

// Values returns the ValueStore interface for this unit of work
func (u *unitOfWork) Values() ValueStore {
	return u.valueStore
}

// Tasks returns the Tasks interface for this unit of work
func (u *unitOfWork) Tasks() Tasks {
	return u.tasks
}

// Run executes a function in a managed goroutine
func (u *unitOfWork) Run(fn func(uw UnitOfWork)) {
	// Don't start new goroutines if already closed or closing
	if u.closed.Load() {
		return
	}

	u.wg.Add(1)
	u.tasks.Add() // Track in task coordinator

	go func() {
		defer u.wg.Done()
		defer u.tasks.Done()

		fn(u)
	}()
}

// Terminate initiates shutdown with error propagation
func (u *unitOfWork) Terminate(err error) error {
	if u.closed.CompareAndSwap(false, true) {
		u.resourceManager.setTerminationError(err)
		u.cancel()
		return u.closeInternal()
	}
	return nil
}

// Close initiates graceful shutdown and waits for all goroutines to finish
func (u *unitOfWork) Close() error {
	if u.closed.CompareAndSwap(false, true) {
		u.cancel() // Signal cancellation to all operations
		return u.closeInternal()
	}
	return nil
}

// closeInternal performs the actual close operations
// This is protected by the closeOnce sync.Once to ensure it happens exactly once
func (u *unitOfWork) closeInternal() error {
	var err error

	u.closeOnce.Do(func() {
		// Wait for all goroutines to finish with closeTimeout
		done := make(chan struct{})
		go func() {
			u.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Normal shutdown
		case <-time.After(u.closeTimeout):
			// Force shutdown after closeTimeout
		}

		err = u.resourceManager.Close()
	})

	return err
}

// AddCleanup registers a function to be called on close
func (u *unitOfWork) AddCleanup(fn func() error) {
	u.resourceManager.AddCleanup(fn)
}

// GetTasks associates coroutine value updates with thread task for later execution.
func GetTasks(provider TaskProvider, updates ...*Update) ([]*Task, error) {
	tasks := make([]*Task, len(updates))
	for i, update := range updates {
		task, err := provider.GetTask(update.State)
		if err != nil {
			return nil, err
		}

		if update.Error != nil {
			task.RaiseError = update.Error
		} else {
			task.Resumed = update.Result
		}

		tasks[i] = task
	}

	return tasks, nil
}

// Context key for UnitOfWork
var unitOfWorkKey = struct{}{}
