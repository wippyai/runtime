package engine

import (
	"context"
	"fmt"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/internal/closer"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type (
	// Layer represents a middleware layer that can process tasks.
	// Layers are executed in order they were added (first added = outermost layer).
	// Each layer receives a CVM interface which can be used to pass tasks to the next layer.
	Layer interface {
		// Step processes tasks and their yields.
		// The CVM parameter represents the next layer (or base CVM) in the chain.
		// Returns processed tasks and any error encountered.
		Step(cvm CVM, tasks ...*Task) ([]*Task, error)
	}

	LayerCloser interface {
		CloseLayer()
	}

	// Contexter allows middleware layers to modify the context chain.
	// Implementing this interface is optional for layers that need to add values to the context.
	Contexter interface {
		WithContext(ctx context.Context) context.Context
	}

	// CVM represents core VM functionality required by layers
	CVM interface {
		Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan Result, error)
		Step(tasks ...*Task) ([]*Task, error)
		GetTasks() []*Task
		GetTask(thread *lua.LState) (*Task, error)
		State() *lua.LState
		Close()
	}

	// CoroutineLeak represents an error when orphaned coroutines are detected
	// during VM execution. Count indicates the number of leaked coroutines.
	CoroutineLeak struct {
		Count int
	}

	// DeadlockError represents a deadlock condition where coroutines are
	// unable to make progress. Count indicates number of blocked coroutines.
	DeadlockError struct {
		Count int
	}

	wrappedLayer struct {
		next  CVM   // Next layer or base CVM
		layer Layer // Current layer
	}
)

func (e *CoroutineLeak) Error() string {
	return fmt.Sprintf("found orphaned coroutines: %d", e.Count)
}

func (e *DeadlockError) Error() string {
	return fmt.Sprintf("deadlock detected on %d coroutines", e.Count)
}

func (w *wrappedLayer) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan Result, error) {
	return w.next.Start(ctx, funcName, args...)
}

func (w *wrappedLayer) GetTask(thread *lua.LState) (*Task, error) {
	return w.next.GetTask(thread)
}

func (w *wrappedLayer) GetTasks() []*Task {
	return w.next.GetTasks()
}

func (w *wrappedLayer) Step(tasks ...*Task) ([]*Task, error) {
	return w.layer.Step(w.next, tasks...)
}

func (w *wrappedLayer) State() *lua.LState { return w.next.State() }

func (w *wrappedLayer) Close() {
	w.next.Close()
}

// RunnerOption represents a function that can modify a Runner
type RunnerOption func(*Runner)

// WithLayer returns a RunnerOption that adds a layer to the wrapper
func WithLayer(layer Layer) RunnerOption {
	return func(w *Runner) {
		w.layers = append(w.layers, layer)
		// Invalidate cache
		w.wrapped = nil
		w.layerCount = 0
	}
}

// Runner provides a way to wrap CVM with middleware layers
type Runner struct {
	cvm        *CoroutineVM // Base CVM
	taskGroup  *TaskGroup   // Keep track of tasks
	layers     []Layer      // Layers in order of addition (first = outermost)
	wrapped    CVM          // Cached wrapped chain
	layerCount int          // Number of layers when cache was built
}

// NewRunner creates a new wrapper around provided CVM with optional layers
func NewRunner(cvm *CoroutineVM, opts ...RunnerOption) *Runner {
	w := &Runner{
		cvm:       cvm,
		taskGroup: NewTaskGroup(256),
		layers:    make([]Layer, 0),
	}

	// Apply all options
	for _, opt := range opts {
		opt(w)
	}

	return w
}

func (e *Runner) GetCVM() CVM {
	return e.cvm
}

// getWrapped returns cached or builds new wrapped chain
func (e *Runner) getWrapped() CVM {
	// Return cached if available and valid
	if e.wrapped != nil && e.layerCount == len(e.layers) {
		return e.wrapped
	}

	// Build new chain starting with base CVM
	wrapped := CVM(e.cvm)

	// Wrap each layer in order (first = outermost)
	for i := 0; i < len(e.layers); i++ {
		wrapped = &wrappedLayer{next: wrapped, layer: e.layers[i]}
	}

	// Cache the result
	e.wrapped = wrapped

	return wrapped
}

// GetTaskGroup returns the task group associated with this runner.
// The task group is used to track and manage concurrent tasks.
func (e *Runner) GetTaskGroup() *TaskGroup {
	return e.taskGroup
}

func (e *Runner) GetLayers() []Layer {
	return e.layers
}

// Start initiates execution of a function with the given name and arguments.
// It returns a channel that will receive the execution result.
func (e *Runner) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan Result, error) {
	return e.getWrapped().Start(ctx, funcName, args...)
}

// Run executes the VM until completion, processing tasks through the layer chain.
// It manages coroutine execution and handles task scheduling.
// Returns the final execution result or an error if execution fails.
func (e *Runner) Run(ctx context.Context, exitCh <-chan Result) (lua.LValue, error) {
	defer func() {
		for _, t := range e.cvm.tasks {
			_ = e.cvm.removeTask(t)
		}
		e.taskGroup.clean()
	}()

	wrapped := e.getWrapped()
	var result Result
	for {
		tasks, err := wrapped.Step(e.cvm.queue.Drain()...)
		if err != nil {
			return nil, err
		}

		if len(tasks) > 0 {
			// some tasks leaked out of the wrapped chain
			return nil, fmt.Errorf("unexpected tasks, missing VM layer: %v", tasks)
		}

		select {
		case result = <-exitCh:
			stuck := len(e.cvm.tasks)
			if result.Error != nil {
				return nil, result.Error
			}

			if len(result.Result) > 0 {
				if stuck > 0 {
					// soft-error, we let parent to decide
					return result.Result[0], &CoroutineLeak{Count: stuck}
				}
				return result.Result[0], nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			// wait-wait-wait, are we deadlocked?
			if len(tasks) == 0 && e.taskGroup.GetTaskCount() == 0 {
				if len(e.cvm.tasks) == 0 {
					return nil, nil
				}

				return nil, &DeadlockError{Count: len(e.cvm.tasks)}
			}
		}

		// wait-wait-wait, are we deadlocked?
		if len(tasks) == 0 && e.taskGroup.GetTaskCount() == 0 {
			if len(e.cvm.tasks) == 0 {
				return nil, nil
			}

			return nil, &DeadlockError{Count: len(e.cvm.tasks)}
		}

		// block for any pending task
		tasks, err = e.taskGroup.Wait(ctx, e.cvm, true)
		if err != nil {
			return nil, err
		}

		for _, task := range tasks {
			e.cvm.queue.Push(task)
		}
	}
}

// Step processes tasks through the layer chain.
func (e *Runner) Step(tasks ...*Task) ([]*Task, error) {
	return e.getWrapped().Step(tasks...)
}

// Continue advances all internal until no longer possible and external signals are needed.
func (e *Runner) Continue(ctx context.Context) error {
	// Check if the context is already canceled.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	wrapped := e.getWrapped()

	// get all tasks from the queue
	tasks, err := wrapped.Step(e.cvm.queue.Drain()...)
	if err != nil {
		return err
	}

	if len(tasks) > 0 {
		// some tasks leaked out of the wrapped chain
		return fmt.Errorf("unexpected tasks, missing VM layer: %v", tasks)
	}

	// wait-wait-wait, are we deadlocked?
	if len(tasks) == 0 && e.taskGroup.GetTaskCount() == 0 {
		if len(e.cvm.tasks) == 0 {
			return nil
		}

		return &DeadlockError{Count: len(e.cvm.tasks)}
	}

	// block for any pending task
	tasks, err = e.taskGroup.Wait(ctx, e.cvm, true)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		// schedule new tasks
		e.cvm.queue.Push(task)
	}

	return nil
}

func (e *Runner) HasTasks() bool {
	return e.cvm.queue.Len() > 0
}

// Execute runs a function through the layer chain with provided context and arguments
func (e *Runner) Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error) {
	// we always have to ensure we run using the task group context!
	ctx, cleanup := closer.WithContext(e.WithContext(ctx))
	defer func() {
		if err := cleanup.Close(); err != nil {
			e.cvm.vm.log.Error("cleanup failed", zap.Error(err))
		}
	}()

	out, err := e.Start(ctx, funcName, args...)
	if err != nil {
		return nil, err
	}

	return e.Run(ctx, out)
}

// WithContext creates a new context with task group and layer-specific values.
// Each layer that implements Contexter can add its own values to the context chain.
func (e *Runner) WithContext(ctx context.Context) context.Context {
	awake := ctx.Value(ctxapi.WakeUpKey)
	if fn, ok := awake.(func()); ok {
		// this is special handling for the cases where we need to wake up the VM thread
		// in parent worker pool, this contract allows us to send signal upstream
		e.taskGroup.wakeupFunc = fn
	}

	ctx = WithTaskGroup(ctx, e.taskGroup)
	for _, l := range e.layers {
		if c, ok := l.(Contexter); ok {
			ctx = c.WithContext(ctx)
		}
	}

	return ctx
}

// Close shuts down the runner and its underlying CVM.
// This should be called when the runner is no longer needed.
func (e *Runner) Close() {
	for _, l := range e.layers {
		if c, ok := l.(LayerCloser); ok {
			c.CloseLayer()
		}
	}

	e.getWrapped().Close()
	e.taskGroup.clean()
	e.taskGroup = nil
	e.layers = nil
	e.layerCount = 0
	e.cvm = nil
}
