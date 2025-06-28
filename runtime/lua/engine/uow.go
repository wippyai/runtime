package engine

import (
	"context"
	"sync"
	"sync/atomic"

	ctxapi "github.com/ponyruntime/pony/api/context"
	lua "github.com/yuin/gopher-lua"
)

const scheduleSize = 32

var unitOfWorkPool = sync.Pool{
	New: func() interface{} {
		return &unitOfWork{
			valueStore:      newValueStore(),
			resourceManager: newResourceManager(),
			tasks:           newTaskCoordinator(scheduleSize, nil),
		}
	},
}

// unitOfWork implements the UnitOfWork interface
type unitOfWork struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	state  *lua.LState // Added to satisfy StateProvider interface

	closed    atomic.Bool // Ensures close happens exactly once
	closeOnce sync.Once   // Additional protection for close operations

	valueStore      *valueStore
	resourceManager *resourceManager
	tasks           *taskCoordinator
}

// NewUnitOfWork creates a new UnitOfWork instance
func NewUnitOfWork(parentCtx context.Context, state *lua.LState) (UnitOfWork, context.Context) {
	// Get from pool
	uw := unitOfWorkPool.Get().(*unitOfWork)

	ctx, cancel := context.WithCancel(parentCtx)

	uw.ctx = ctx
	uw.cancel = cancel
	uw.state = state
	uw.closed.Store(false)

	// Check for wake-up function in context
	if awake := parentCtx.Value(ctxapi.WakeUpKey); awake != nil {
		if fn, ok := awake.(func()); ok {
			uw.tasks.wakeupFunc = fn
		}
	}

	// Add cleanup for state context
	if state != nil {
		state.SetContext(ctx)
		uw.AddCleanup(func() error {
			if state.Context() != nil {
				state.SetContext(context.Background())
			}
			return nil
		})
	}

	// Store in context
	ctx = context.WithValue(ctx, unitOfWorkKey, uw)
	uw.ctx = ctx

	return uw, ctx
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

		cerr := u.closeInternal()

		u.reset()
		unitOfWorkPool.Put(u)

		return cerr
	}

	return nil
}

// Close initiates graceful shutdown and waits for all goroutines to finish
func (u *unitOfWork) Close() error {
	if u.closed.CompareAndSwap(false, true) {
		u.cancel() // Signal cancellation to all operations
		err := u.closeInternal()

		u.reset()
		unitOfWorkPool.Put(u)

		return err
	}

	return nil
}

// closeInternal performs the actual close operations
// This is protected by the closeOnce sync.Once to ensure it happens exactly once
func (u *unitOfWork) closeInternal() error {
	var err error

	u.closeOnce.Do(func() {
		u.wg.Wait()
		err = u.resourceManager.Close()
	})

	return err
}

// AddCleanup registers a function to be called on close
func (u *unitOfWork) AddCleanup(fn func() error) context.CancelFunc {
	return u.resourceManager.AddCleanup(fn)
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

func (u *unitOfWork) reset() {
	u.ctx = nil
	u.cancel = nil
	u.state = nil

	// Reset synchronization primitives
	u.closed.Store(false)
	u.closeOnce = sync.Once{}
	u.wg = sync.WaitGroup{}

	// Reset internal components
	u.valueStore.reset()
	u.resourceManager.reset()
	u.tasks.reset()
}

// Context key for UnitOfWork
type unitOfWorkKeyType struct{}

var unitOfWorkKey = unitOfWorkKeyType{}
