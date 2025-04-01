package engine

import (
	"context"
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

type wrappedLayer struct {
	next  CVM   // Next layer or base CVM
	layer Layer // Current layer
}

func (w *wrappedLayer) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan *Update, error) {
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

		// invalidate cache
		w.wrapped = nil
		w.layerCount = 0
	}
}

// Runner provides a way to wrap CVM with middleware layers
type Runner struct {
	cvm        *CoroutineVM // Base CVM
	layers     []Layer      // Layers in order of addition (first = outermost)
	wrapped    CVM          // Cached wrapped chain
	layerCount int          // Number of layers when cache was built
}

// NewRunner creates a new wrapper around provided CVM with optional layers
func NewRunner(cvm *CoroutineVM, opts ...RunnerOption) *Runner {
	w := &Runner{
		cvm:    cvm,
		layers: make([]Layer, 0),
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

func (e *Runner) QueueLen() int {
	return e.cvm.queue.Len()
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

// Start initiates execution of a function with the given name and arguments.
// It returns a channel that will receive the execution result.
func (e *Runner) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan *Update, error) {
	return e.getWrapped().Start(ctx, funcName, args...)
}

// Run executes the VM until completion, processing threads through the layer chain.
// It manages coroutine execution and handles task scheduling.
// Returns the final execution result or an error if execution fails.
func (e *Runner) Run(ctx context.Context, exitCh <-chan *Update) (lua.LValue, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	uw := GetUnitOfWork(ctx)
	if uw == nil {
		return nil, fmt.Errorf("unit of work not found")
	}

	wrapped := e.getWrapped()
	var result *Update
	for {
		tasks, err := wrapped.Step(e.cvm.queue.Drain()...)
		if err != nil {
			return nil, err
		}

		select {
		case result = <-exitCh:
			stuck := len(e.cvm.threads)
			if result.Error != nil {
				return nil, result.Error
			}

			if len(result.Result) > 0 {
				if stuck > 0 {
					// soft-error, we let pcallFrom to decide
					return result.Result[0], &CoroutineLeak{Count: stuck}
				}

				return result.Result[0], nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if len(tasks) == 0 && e.cvm.queue.Len() == 0 && uw.Tasks().Ready() == 0 && uw.Tasks().Blocked() == 0 {
			return nil, &DeadlockError{Count: len(e.cvm.threads)}
		}

		for _, task := range tasks {
			e.cvm.queue.Push(task)
		}

		updates, err := uw.Tasks().Wait(ctx,
			len(tasks) == 0 &&
				e.cvm.queue.Len() == 0 &&
				uw.Tasks().Ready() == 0,
		)
		if err != nil {
			return nil, err
		}

		newTasks, err := GetTasks(e.cvm, updates...)
		if err != nil {
			return nil, err
		}

		for _, task := range newTasks {
			e.cvm.queue.Push(task)
		}
	}
}

// Step processes threads through the layer chain.
func (e *Runner) Step(tasks ...*Task) ([]*Task, error) {
	return e.getWrapped().Step(tasks...)
}

// Continue advances all internal until no longer possible and external signals are needed.
func (e *Runner) Continue(ctx context.Context, block bool) error {
	// Check if the context is already canceled.
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	uw := GetUnitOfWork(ctx)
	if uw == nil {
		return fmt.Errorf("unit of work not found")
	}

	// get all threads from the queue
	tasks, err := e.getWrapped().Step(e.cvm.queue.Drain()...)

	if err != nil {
		return err
	}

	if len(tasks) > 0 {
		// some threads leaked out of the wrapped chain
		return fmt.Errorf("unexpected threads, missing VM layer: %v", tasks)
	}

	if len(e.cvm.threads) == 0 {
		return nil
	}

	// wait-wait-wait, are we deadlocked?
	if uw.Tasks().Blocked() == 0 && uw.Tasks().Ready() == 0 {
		return &DeadlockError{Count: len(e.cvm.threads)}
	}

	// block for any pending task
	// st := time.Now()
	updates, err := uw.Tasks().Wait(ctx, block)
	if err != nil {
		return err
	}
	// log.Printf("step wait time: %v", time.Since(st))

	tasks, err = GetTasks(e.cvm, updates...)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		// schedule new threads
		e.cvm.queue.Push(task)
	}

	return nil
}

// Execute runs a function through the layer chain with provided context and arguments
func (e *Runner) Execute(ctx context.Context, funcName string, args ...lua.LValue) (lua.LValue, error) {
	var finalErr error
	nuw, ctx := e.InitUnitOfWork(ctx)
	defer func() {
		if finalErr != nil {
			if err := nuw.Terminate(finalErr); err != nil {
				e.cvm.vm.log.Error("unit of work termination failed", zap.Error(err))
			}

			return
		}

		if err := nuw.Close(); err != nil {
			e.cvm.vm.log.Error("unit of work closing failed", zap.Error(err))
		}
	}()

	out, err := e.Start(ctx, funcName, args...)
	if err != nil {
		finalErr = err
		return nil, err
	}

	r, err := e.Run(ctx, out)
	if err != nil {
		finalErr = err
		return nil, err
	}

	return r, nil
}

func (e *Runner) InitUnitOfWork(ctx context.Context) (UnitOfWork, context.Context) {
	uw, ctx := NewUnitOfWork(ctx, e.cvm.vm.state)
	for _, l := range e.layers {
		if c, ok := l.(Initiater); ok {
			c.InitUnitOfWork(uw)
		}
	}

	return uw, ctx
}

// Close shuts down the runner and its underlying CVM.
// This should be called when the runner is no longer needed.
func (e *Runner) Close() {
	e.getWrapped().Close()
	e.layers = nil
	e.layerCount = 0
	e.cvm = nil
}
