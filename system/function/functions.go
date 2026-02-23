// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"fmt"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type Registry struct {
	ctx        context.Context
	bus        event.Bus
	logger     *zap.Logger
	subscriber *eventbus.Subscriber
	handlers   sync.Map
	options    sync.Map
}

const functionEventPattern = "function.(register|delete)"

// NewFunctionRegistry creates a new Registry instance with the provided event bus and logger.
func NewFunctionRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Registry{
		bus:      bus,
		logger:   logger,
		handlers: sync.Map{},
		options:  sync.Map{},
	}
}

// Start initializes the executor and begins listening for executor events.
// It sets up a subscriber for handling executor-related events on the event bus.
func (f *Registry) Start(ctx context.Context) error {
	f.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		f.ctx,
		f.bus,
		function.System,
		functionEventPattern,
		f.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	f.subscriber = sub

	return nil
}

// Stop cleanly shuts down the executor by closing its event subscriber.
func (f *Registry) Stop() error {
	if f.subscriber != nil {
		f.subscriber.Close()
	}
	return nil
}

func (f *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case function.FunctionRegister:
		f.registerFunction(e)
	case function.FunctionDelete:
		f.deleteFunction(e)
	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (f *Registry) registerFunction(e event.Event) {
	reg, ok := e.Data.(*function.FuncEntry)
	if !ok {
		f.logger.Error("invalid register function payload",
			zap.String("function", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid register function payload")
		return
	}

	id := registry.ParseID(e.Path)

	// Store the function handler
	f.handlers.Store(id, reg.Handler)

	// Store options if provided
	if reg.Options != nil {
		f.options.Store(id, reg.Options)
	} else {
		// Remove options if nil (handles updates that clear options)
		f.options.Delete(id)
	}

	f.logger.Debug("function registered", zap.String("function", e.Path))
	f.sendAccept(e.Path)
}

func (f *Registry) deleteFunction(e event.Event) {
	id := registry.ParseID(e.Path)

	// Check if the function exists before removing
	_, exists := f.handlers.Load(id)
	if !exists {
		f.logger.Warn("function not found", zap.String("function", e.Path))
		f.sendReject(e.Path, "function not found")
		return
	}

	// Remove the function handler
	f.handlers.Delete(id)

	// Remove associated options
	f.options.Delete(id)

	f.logger.Debug("function removed", zap.String("function", e.Path))
	f.sendAccept(e.Path)
}

func (f *Registry) sendAccept(path event.Path) {
	f.sendResponse(path, function.FunctionAccept, nil)
}

func (f *Registry) sendReject(path event.Path, reason string) {
	f.sendResponse(path, function.FunctionReject, reason)
}

func (f *Registry) sendResponse(path event.Path, kind event.Kind, data any) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   kind,
		Path:   path,
		Data:   data,
	})
}

// Call runs the given task using its registered handler synchronously.
// Returns an error if no handler is registered for the task's target or if the handler type is invalid.
// Blocks until execution completes or context is canceled.
func (f *Registry) Call(ctx context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
	if ctx == nil {
		return nil, function.ErrNilContext
	}

	handler, exists := f.handlers.Load(task.ID)
	if !exists {
		return nil, NewHandlerNotFoundError(task.ID)
	}

	execHandler, ok := handler.(function.Func)
	if !ok {
		return nil, NewInvalidHandlerError(task.ID)
	}

	// Merge preset and runtime options into task.Options before calling interceptors
	var bag runtimeapi.Bag
	if storedOptions, ok := f.options.Load(task.ID); ok {
		bag, _ = storedOptions.(runtimeapi.Bag)
	}
	if runtimeBag, ok := task.Options.(runtimeapi.Bag); ok {
		if bag != nil {
			bag = bag.Merge(runtimeBag)
		} else {
			bag = runtimeBag
		}
	}
	if bag != nil {
		task.Options = bag
	}

	// Create executor wrapper that will be called by chain or directly
	executorFunc := func(ctx context.Context, task runtimeapi.Task) (*runtimeapi.Result, error) {
		return f.executor(ctx, execHandler, task)
	}

	// Execute through interceptor chain if available
	if interceptors := function.GetInterceptorRegistry(ctx); interceptors != nil {
		return interceptors.Execute(ctx, executorFunc, task)
	}

	return executorFunc(ctx, task)
}

// executor creates frame context and executes the function handler.
// This is called as the final step in the interceptor chain or directly if no chain exists.
// PID is generated with Host set to the function ID - each function is its own mini-host.
func (f *Registry) executor(ctx context.Context, handler function.Func, task runtimeapi.Task) (*runtimeapi.Result, error) {
	// Open frame context with inheritance from sealed parent (actor, scope, etc.)
	ctx, fc := ctxapi.OpenFrameContext(ctx)

	// Generate PID with function ID as Host - function is its own host for message routing
	gen := process.GetPIDGenerator(ctx)
	if gen == nil {
		ctxapi.ReleaseFrameContext(fc)
		return nil, function.ErrPIDGeneratorNotFound
	}
	pid := gen.Generate(task.ID.String())

	// Build pairs slice with capacity for base pairs + task context.
	pairs := make([]ctxapi.Pair, 0, 2+len(task.Context))
	pairs = append(pairs,
		ctxapi.Pair{Key: runtimeapi.FrameIDKey, Value: task.ID},
		ctxapi.Pair{Key: runtimeapi.FramePIDKey, Value: pid},
	)
	pairs = append(pairs, task.Context...)

	if err := fc.SetMultiple(pairs...); err != nil {
		ctxapi.ReleaseFrameContext(fc)
		return nil, NewFrameContextError(err)
	}

	// Execute function handler
	result, err := handler(ctx, task)

	// Release frame back to pool
	ctxapi.ReleaseFrameContext(fc)

	return result, err
}

// GetOptions returns registered default options for a function ID.
// The returned bag is cloned to prevent callers from mutating registry state.
func (f *Registry) GetOptions(id registry.ID) (runtimeapi.Bag, bool) {
	raw, ok := f.options.Load(id)
	if !ok {
		return nil, false
	}

	bag, ok := raw.(runtimeapi.Bag)
	if !ok || bag == nil {
		return nil, false
	}

	cloned, ok := bag.Clone().(runtimeapi.Bag)
	if !ok {
		return nil, false
	}

	return cloned, true
}

// Ensure Registry implements the operation.Registry interface
var _ function.Registry = (*Registry)(nil)
