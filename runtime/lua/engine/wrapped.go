package engine

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"log"
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
		GetContext() context.Context
		Start(funcName string, args ...lua.LValue) (<-chan TaskResult, error)
		Step(tasks ...*Task) ([]*Task, error)
		GetTasks() []*Task
		GetTask(thread *lua.LState) (*Task, error)
		Close()
	}

	wrappedLayer struct {
		next  CVM   // Next layer or base CVM
		layer Layer // Current layer
	}
)

func (w *wrappedLayer) GetContext() context.Context {
	return w.next.GetContext()
}

func (w *wrappedLayer) Start(funcName string, args ...lua.LValue) (<-chan TaskResult, error) {
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

func (w *wrappedLayer) Close() {
	w.next.Close()
}

// CVMOption represents a function that can modify a CVMWrapper
type CVMOption func(*CVMWrapper)

// WithLayer returns a CVMOption that adds a layer to the wrapper
func WithLayer(layer Layer) CVMOption {
	return func(w *CVMWrapper) {
		w.layers = append(w.layers, layer)
		// Invalidate cache
		w.wrapped = nil
		w.layerCount = 0
	}
}

// CVMWrapper provides a way to wrap CVM with middleware layers
type CVMWrapper struct {
	cvm         *CoroutineVM    // Base CVM
	layers      []Layer         // Layers in order of addition (first = outermost)
	wrapped     CVM             // Cached wrapped chain
	layerCount  int             // Number of layers when cache was built
	blockNotify chan LayerState // Ready channel
	blocked     map[Layer]int
}

// NewWrappedCVM creates a new wrapper around provided CVM with optional layers
func NewWrappedCVM(cvm *CoroutineVM, opts ...CVMOption) *CVMWrapper {
	w := &CVMWrapper{
		cvm:     cvm,
		layers:  make([]Layer, 0),
		blocked: make(map[Layer]int),
	}

	// Apply all options
	for _, opt := range opts {
		opt(w)
	}

	// single message for block and single for blockNotify
	w.blockNotify = make(chan LayerState, len(w.layers)*2)

	// Check if any layer is blocking
	found := false
	for _, layer := range w.layers {
		if bLayer, ok := layer.(Blockable); ok {
			// we always want to know who is currently blocked by any external data
			bLayer.SetNotify(w.blockNotify)
			found = true

			w.blocked[layer] = 0
		}
	}

	if !found {
		w.blockNotify = nil
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

// Execute runs a function through the layer chain with provided context and arguments
func (e *CVMWrapper) Execute(
	ctx context.Context,
	funcName string,
	args ...lua.LValue,
) (lua.LValue, error) {
	if ctx != nil {
		ctx, cleanup := closer.WithContext(ctx)
		defer func() {
			e.cvm.vm.state.RemoveContext()
			if err := cleanup.Close(); err != nil {
				e.cvm.vm.log.Error("cleanup failed",
					zap.String("function", funcName),
					zap.Error(err))
			}
		}()
		e.cvm.vm.state.SetContext(ctx)
	}

	// Get or build wrapped chain
	wrapped := e.getWrapped()

	out, err := wrapped.Start(funcName, args...)
	if err != nil {
		return nil, err
	}

	for {
		log.Printf("step")
		tasks, err := wrapped.Step(e.cvm.queue.Drain()...)
		if err != nil {
			return nil, err
		}

		if len(tasks) > 0 {
			// some tasks leaked out of the wrapped chain
			return nil, fmt.Errorf("unexpected tasks, missing VM layer: %v", tasks)
		}

		if !e.waitBlocking() {
			break
		}
	}

	// Get final result
	var result TaskResult
	select {
	case result = <-out:
	default:
		if len(e.cvm.tasks) > 0 {
			return nil, errors.New("no result, VM deadlock")
		}
		return nil, errors.New("no result")
	}

	if result.Error != nil {
		return nil, result.Error
	}

	if len(result.Result) > 0 {
		return result.Result[0], nil
	}

	return nil, nil
}

func (e *CVMWrapper) Close() {
	e.getWrapped().Close()
}

func (e *CVMWrapper) GetBlocked() map[Layer]int {
	return e.blocked
}

// waitBlocking updates to use the new non-blocking approach
func (e *CVMWrapper) waitBlocking() bool {
	if e.blockNotify == nil {
		log.Printf("no blockNotify")
		return false
	}

	return e.waitBlocked()
}

// waitBlocked processes all unblocked layers and returns true if we unblocked anything
func (e *CVMWrapper) waitBlocked() bool {
	didUnblock := false
	didBlock := false
	reading := true
	for {
		if !reading {
			break
		}
		select {
		case st := <-e.blockNotify:
			e.blocked[st.Layer] = st.Tasks
			didUnblock = !st.Blocked
		case <-e.cvm.GetContext().Done():
			return false
		default:
			reading = false
		}
	}

	if didUnblock {
		log.Printf("unblocked")
		// we unblocked something and we can continue, layers will mix tasks on themselves now
		return true
	}

	hasTasks := false
	for _, tasks := range e.blocked {
		if tasks > 0 {
			hasTasks = true
			break
		}
	}

	if !didBlock && !hasTasks {
		// nothing really to do
		return false
	}

	log.Printf("fallback")
	log.Printf("blocked: %v", e.blocked)

	// we have to wait for something to unblock
	for {
		select {
		case st := <-e.blockNotify:
			e.blocked[st.Layer] = st.Tasks
			if !st.Blocked {
				return true
			}
		case <-e.cvm.GetContext().Done():
			log.Printf("context done")
			return false
		}
	}
}
