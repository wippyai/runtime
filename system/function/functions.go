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
	logger     *zap.Logger
	bus        event.Bus
	handlers   sync.Map
	options    sync.Map
	subscriber *eventbus.Subscriber
}

// NewFunctionRegistry creates a new Registry instance with the provided event bus and logger.
func NewFunctionRegistry(bus event.Bus, logger *zap.Logger) *Registry {
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
		"function.(register|delete)",
		f.handleEvent,
	)
	if err != nil {
		return function.NewSubscriberError(err)
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
	case function.Register:
		f.registerFunction(e)
	case function.Delete:
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
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.Accept,
		Path:   path,
	})
}

func (f *Registry) sendReject(path event.Path, reason string) {
	f.bus.Send(f.ctx, event.Event{
		System: function.System,
		Kind:   function.Reject,
		Path:   path,
		Data:   reason,
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
		return nil, function.NewHandlerNotFoundError(task.ID)
	}

	execHandler, ok := handler.(function.Func)
	if !ok {
		return nil, function.NewInvalidHandlerError(task.ID)
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
	if registry := function.GetInterceptorRegistry(ctx); registry != nil {
		return registry.Execute(ctx, executorFunc, task)
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

	// Fast path: no task context overrides (most common case)
	if len(task.Context) == 0 {
		_ = fc.Set(runtimeapi.FrameIDKey, task.ID)
		_ = fc.Set(runtimeapi.FramePIDKey, pid)
	} else {
		pairsLen := 2 + len(task.Context)
		pairs := make([]ctxapi.Pair, pairsLen)
		pairs[0] = ctxapi.Pair{Key: runtimeapi.FrameIDKey, Value: task.ID}
		pairs[1] = ctxapi.Pair{Key: runtimeapi.FramePIDKey, Value: pid}
		copy(pairs[2:], task.Context)

		if err := fc.SetMultiple(pairs...); err != nil {
			ctxapi.ReleaseFrameContext(fc)
			return nil, function.NewFrameContextError(err)
		}
	}

	// Execute function handler
	result, err := handler(ctx, task)

	// Release frame back to pool
	ctxapi.ReleaseFrameContext(fc)

	return result, err
}

// Ensure Registry implements the operation.Registry interface
var _ function.Registry = (*Registry)(nil)
