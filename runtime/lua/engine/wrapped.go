package engine

import (
	"context"
	"fmt"
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

	// CVM represents core VM functionality required by layers
	CVM interface {
		Context() context.Context
		Start(funcName string, args ...lua.LValue) (<-chan Result, error)
		Step(tasks ...*Task) ([]*Task, error)
		GetTasks() []*Task
		GetTask(thread *lua.LState) (*Task, error)
		State() *lua.LState
		Close()
	}

	CoroutineLeak struct {
		Count int
	}

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

func (w *wrappedLayer) Context() context.Context {
	return w.next.Context()
}

func (w *wrappedLayer) Start(funcName string, args ...lua.LValue) (<-chan Result, error) {
	return w.next.Start(funcName, args...)
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

// WrapperOption represents a function that can modify a CVMWrapper
type WrapperOption func(*CVMWrapper)

// WithLayer returns a WrapperOption that adds a layer to the wrapper
func WithLayer(layer Layer) WrapperOption {
	return func(w *CVMWrapper) {
		w.layers = append(w.layers, layer)
		// Invalidate cache
		w.wrapped = nil
		w.layerCount = 0
	}
}

// CVMWrapper provides a way to wrap CVM with middleware layers
type CVMWrapper struct {
	cvm        *CoroutineVM // Base CVM
	taskGroup  *TaskGroup   // Keep track of tasks
	layers     []Layer      // Layers in order of addition (first = outermost)
	wrapped    CVM          // Cached wrapped chain
	layerCount int          // Number of layers when cache was built
}

// NewWrappedCVM creates a new wrapper around provided CVM with optional layers
func NewWrappedCVM(cvm *CoroutineVM, opts ...WrapperOption) *CVMWrapper {
	w := &CVMWrapper{
		cvm:       cvm,
		taskGroup: NewTaskGroup(4096), // todo; move to options too
		layers:    make([]Layer, 0),
	}

	// Apply all options
	for _, opt := range opts {
		opt(w)
	}

	return w
}

// getWrapped returns cached or builds new wrapped chain
func (e *CVMWrapper) getWrapped() CVM {
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

func (e *CVMWrapper) GetTaskGroup() *TaskGroup {
	return e.taskGroup
}

func (e *CVMWrapper) Start(funcName string, args ...lua.LValue) (<-chan Result, error) {
	return e.getWrapped().Start(funcName, args...)
}

func (e *CVMWrapper) Run(ctx context.Context, exitCh <-chan Result) (lua.LValue, error) {
	ctx, cleanup := closer.WithContext(WithTaskGroup(ctx, e.taskGroup))
	defer func() {
		for _, t := range e.cvm.tasks {
			_ = e.cvm.removeTask(t)
		}
		e.taskGroup.clean()
		e.cvm.vm.state.RemoveContext()
		if err := cleanup.Close(); err != nil {
			e.cvm.vm.log.Error("cleanup failed", zap.Error(err))
		}
	}()

	// establish context
	e.cvm.vm.state.SetContext(ctx)
	for _, t := range e.cvm.tasks {
		if t.thread.Context() == nil {
			t.thread.SetContext(ctx)
		}
	}

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
			// Wait-Wait-Wait, are we deadlocked?
			if len(tasks) == 0 && e.taskGroup.GetTaskCount() == 0 {
				return nil, &DeadlockError{Count: len(e.cvm.tasks)}
			}
		}

		// Wait-Wait-Wait, are we deadlocked?
		if len(tasks) == 0 && e.taskGroup.GetTaskCount() == 0 {
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

// Execute runs a function through the layer chain with provided context and arguments
func (e *CVMWrapper) Execute(
	ctx context.Context,
	funcName string,
	args ...lua.LValue,
) (lua.LValue, error) {
	out, err := e.Start(funcName, args...)
	if err != nil {
		return nil, err
	}

	return e.Run(ctx, out)
}

func (e *CVMWrapper) Close() {
	e.getWrapped().Close()
}
