package functions

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// FunctionRegistry manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type FunctionRegistry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

// NewExecutor creates a new FunctionRegistry instance with the provided event bus and logger.
func NewExecutor(bus events.Bus, logger *zap.Logger) *FunctionRegistry {
	return &FunctionRegistry{
		bus:      bus,
		logger:   logger,
		handlers: sync.Map{},
	}
}

// Start initializes the executor and begins listening for executor events.
// It sets up a subscriber for handling executor-related events on the event bus.
func (f *FunctionRegistry) Start(ctx context.Context) error {
	f.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		f.ctx,
		f.bus,
		function.System,
		"function.(register|remove)",
		f.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	f.subscriber = sub

	return nil
}

// Stop cleanly shuts down the executor by closing its event subscriber.
func (f *FunctionRegistry) Stop() error {
	if f.subscriber != nil {
		f.subscriber.Close()
	}
	return nil
}

func (f *FunctionRegistry) handleEvent(e events.Event) {
	switch e.Kind {
	case function.RegisterFunctionHandler:
		f.registerFunction(e)
	case function.DeleteFunctionHandler:
		f.deleteFunction(e)
	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (f *FunctionRegistry) registerFunction(e events.Event) {
	fn, ok := e.Data.(function.Func)
	if !ok {
		f.logger.Error("invalid register function payload",
			zap.String("function", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid register function payload")
		return
	}

	// Store the function
	f.handlers.Store(registry.ParseID(e.Path), fn)
	f.logger.Debug("function registered", zap.String("function", e.Path))

	f.sendAccept(e.Path)
}

func (f *FunctionRegistry) deleteFunction(e events.Event) {
	// Check if the function exists before removing
	_, exists := f.handlers.Load(registry.ParseID(e.Path))
	if !exists {
		f.logger.Warn("function not found", zap.String("function", e.Path))
		f.sendReject(e.Path, "function not found")
		return
	}

	// Remove the function
	f.handlers.Delete(registry.ParseID(e.Path))
	f.logger.Debug("function removed", zap.String("function", e.Path))

	f.sendAccept(e.Path)
}

func (f *FunctionRegistry) sendAccept(path events.Path) {
	f.bus.Send(f.ctx, events.Event{
		System: function.System,
		Kind:   function.AcceptFunction,
		Path:   path,
	})
}

func (f *FunctionRegistry) sendReject(path events.Path, reason string) {
	f.bus.Send(f.ctx, events.Event{
		System: function.System,
		Kind:   function.RejectFunction,
		Path:   path,
		Data:   reason,
	})
}

// Call runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (f *FunctionRegistry) Call(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := f.handlers.Load(task.Handler)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.Handler)
	}

	// keep context boundaries
	if ctx == nil {
		ctx = context.Background()
	}

	ctx = context.WithValue(ctx, contextapi.HandlerCtx, task.Handler)
	execHandler, ok := handler.(function.Func)
	if !ok {
		return nil, fmt.Errorf("invalid handler type for target: %s", task.Handler)
	}

	return execHandler(ctx, task)
}
