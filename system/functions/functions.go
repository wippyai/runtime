package functions

import (
	"context"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
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
		bus:    bus,
		logger: logger,
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
		runtime.FunctionSystem,
		"functions.*",
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
	case runtime.RegisterFunctionCommand:
		f.handleRegisterFunction(e)
	case runtime.DeleteFunctionCommand:
		f.handleDeleteFunction(e)
	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", string(e.Kind)),
			zap.String("path", string(e.Path)))
	}
}

func (f *FunctionRegistry) handleRegisterFunction(e events.Event) {
	reg, ok := e.Data.(runtime.RegisterFunc)
	if !ok {
		f.logger.Error("invalid register function payload",
			zap.String("function", string(e.Path)),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid register function payload")
		return
	}

	// Store the function
	f.handlers.Store(reg.ID.String(), reg.Func)
	f.logger.Debug("function registered", zap.String("function", reg.ID.String()))

	f.sendAccept(e.Path)
}

func (f *FunctionRegistry) handleDeleteFunction(e events.Event) {
	del, ok := e.Data.(runtime.DeleteFunc)
	if !ok {
		f.logger.Error("invalid delete function payload",
			zap.String("function", string(e.Path)),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		f.sendReject(e.Path, "invalid delete function payload")
		return
	}

	// Check if the function exists before removing
	_, exists := f.handlers.Load(del.ID.String())
	if !exists {
		f.logger.Warn("function not found", zap.String("function", del.ID.String()))
		f.sendReject(e.Path, "function not found")
		return
	}

	// Remove the function
	f.handlers.Delete(del.ID.String())
	f.logger.Debug("function removed", zap.String("function", del.ID.String()))

	f.sendAccept(e.Path)
}

func (f *FunctionRegistry) sendAccept(path events.Path) {
	f.bus.Send(f.ctx, events.Event{
		System: runtime.FunctionSystem,
		Kind:   runtime.AcceptFunction,
		Path:   path,
	})
}

func (f *FunctionRegistry) sendReject(path events.Path, reason string) {
	f.bus.Send(f.ctx, events.Event{
		System: runtime.FunctionSystem,
		Kind:   runtime.RejectFunction,
		Path:   path,
		Data:   reason,
	})
}

// Call runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (f *FunctionRegistry) Call(task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := f.handlers.Load(task.Target.String())
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.Target)
	}

	// keep context boundaries
	if task.Context == nil {
		task.Context = context.Background()
	}

	task.Context = context.WithValue(task.Context, contextapi.HandlerCtx, task.Target)

	execHandler, ok := handler.(runtime.Func)
	if !ok {
		return nil, fmt.Errorf("invalid handler type for target: %s", task.Target)
	}

	return execHandler(task)
}
