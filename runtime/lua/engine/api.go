// Package engine provides a coroutine-based execution environment for Lua scripts
// with middleware layers, resource management, and concurrency primitives.
package engine

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

//------------------------------------------------------------------------------
// Core Update Type
//------------------------------------------------------------------------------

// Update represents the outcome of a coroutine execution.
// It contains the Lua state, return values, and any error that occurred.
type Update struct {
	State  *lua.LState  // Lua state associated with this update
	Result []lua.LValue // Return values from the coroutine
	Error  error        // Error that occurred during execution, if any
}

// NewUpdate creates a new Update instance with the provided state, results, and error.
func NewUpdate(l *lua.LState, res []lua.LValue, err error) *Update {
	return &Update{
		State:  l,
		Result: res,
		Error:  err,
	}
}

//------------------------------------------------------------------------------
// Core Interfaces
//------------------------------------------------------------------------------

// ValueStore provides thread-safe storage for arbitrary values.
// It enables safe concurrent access to shared data within the VM.
type ValueStore interface {
	// Get retrieves a value by its key.
	// Returns the value and a boolean indicating whether the key was found.
	Get(key any) (any, bool)

	// Set stores a value with the given key.
	Set(key any, value any)

	// Delete removes a value with the given key.
	Delete(key any)

	// GetOrStore retrieves an existing value or stores a new one.
	// Returns the value (either existing or new) and a boolean indicating whether the value was loaded.
	GetOrStore(key any, value any) (any, bool)

	// CompareAndSwap performs atomic compare-and-swap operation.
	// Returns true if the swap was successful.
	CompareAndSwap(key any, old any, new any) bool
}

// Tasks manages coroutine coordination and task lifecycle.
// It provides methods for tracking active threads and sending signals between them.
type Tasks interface {
	// Add registers a new task.
	// Increments the task counter to track an active operation.
	Add()

	// Done signals that a task has completed.
	Done()

	// WakeUp signals that threads are ready to be processed. Usually triggered to handle deliveries to internal layers.
	// This is thread-safe and can be called from any goroutine.
	WakeUp()

	// Wait processes results and returns threads ready for resumption.
	// If block is true, it will wait for at least one result or wake-up signal.
	Wait(ctx context.Context, block bool) ([]*Update, error)

	// Send pushes a result into the task group, will be passed to VM runner on next step.
	// This is thread-safe and can be called from any goroutine. Scheduling processing.
	Send(ctx context.Context, result *Update) error

	// Schedule adds a function to be executed in the task group on task polling (Wait).
	Schedule(func()) error

	// Count returns the current number of active threads.
	Count() int
}

// StateProvider interface for components that have an associated Lua state
type StateProvider interface {
	// State returns the associated Lua state
	State() *lua.LState
}

// Initiater interface for components that can initialize a unit of work
type Initiater interface {
	// InitUnitOfWork initializes a new unit of work with the provided instance
	InitUnitOfWork(UnitOfWork)
}

// UnitOfWork combines resource management, value storage, and task coordination.
// It provides a comprehensive execution context for coroutines and related goroutines,
// ensuring proper cleanup and synchronization.
type UnitOfWork interface {
	StateProvider

	// Context returns the managed context associated with this unit of work.
	Context() context.Context

	// Run executes a function in a managed goroutine with proper task tracking.
	// The function receives this UnitOfWork instance for state access.
	Run(fn func(uw UnitOfWork))

	// AddCleanup registers a function to be called when the unit of work is closed.
	// Cleanup functions are called in reverse order (LIFO).
	AddCleanup(fn func() error) // todo: we need a proper eviction func as well!

	// Terminate initiates shutdown with error propagation.
	// It cancels the context and executes cleanup functions.
	Terminate(err error) error

	// Close initiates graceful shutdown and waits for all goroutines to finish.
	// It executes all cleanup functions and returns the first error encountered.
	Close() error

	// Values returns the ValueStore interface for this unit of work
	Values() ValueStore

	// Tasks returns the Tasks interface for this unit of work
	Tasks() Tasks
}

//------------------------------------------------------------------------------
// Layer Interfaces
//------------------------------------------------------------------------------

// Layer represents a middleware layer that can process threads.
// Layers are executed in order they were added (first added = outermost layer).
// Each layer receives a CVM interface which can be used to pass threads to the next layer.
type Layer interface {
	// Step processes threads and their yields.
	// The CVM parameter represents the next layer (or base CVM) in the chain.
	// Returns processed threads and any error encountered.
	Step(cvm CVM, tasks ...*Task) ([]*Task, error)
}

// TaskProvider interface for components that can retrieve threads by Lua state
type TaskProvider interface {
	// GetTask retrieves a task associated with the given Lua state.
	// Returns an error if the task is not found.
	GetTask(thread *lua.LState) (*Task, error)
}

// CVM represents core VM functionality required by layers.
// It provides methods for task management and execution.
type CVM interface {
	StateProvider
	TaskProvider

	// Start initiates execution of a named function with the provided arguments.
	// Returns a channel that will receive updates about the execution.
	Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan *Update, error)

	// Step advances the execution of threads.
	// Returns yielded threads and any error encountered.
	Step(tasks ...*Task) ([]*Task, error)

	// GetTasks returns all threads currently running in the VM.
	GetTasks() []*Task

	// Close cleans up resources and terminates all running threads.
	Close()
}

//------------------------------------------------------------------------------
// Error Types
//------------------------------------------------------------------------------

// CoroutineLeak represents an error when orphaned coroutines are detected
// during VM execution. Count indicates the number of leaked coroutines.
type CoroutineLeak struct {
	Count int // Number of leaked coroutines
}

// Error returns a string representation of the CoroutineLeak error.
func (e *CoroutineLeak) Error() string {
	return fmt.Sprintf("found orphaned coroutines: %d", e.Count)
}

// DeadlockError represents a deadlock condition where coroutines are
// unable to make progress. Count indicates number of blocked coroutines.
type DeadlockError struct {
	Count int // Number of blocked coroutines
}

// Error returns a string representation of the DeadlockError error.
func (e *DeadlockError) Error() string {
	return fmt.Sprintf("deadlock detected on %d coroutines", e.Count)
}
