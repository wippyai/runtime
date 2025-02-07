package functions

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// FunctionExecutor manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type FunctionExecutor struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

// NewExecutor creates a new FunctionExecutor instance with the provided event bus and logger.
func NewExecutor(bus events.Bus, logger *zap.Logger) *FunctionExecutor {
	return &FunctionExecutor{
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the executor and begins listening for executor events.
// It sets up a subscriber for handling executor-related events on the event bus.
func (e *FunctionExecutor) Start(ctx context.Context) error {
	e.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		e.ctx,
		e.bus,
		runtime.FunctionSystem,
		"functions.*",
		e.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	e.subscriber = sub

	return nil
}

// Stop cleanly shuts down the executor by closing its event subscriber.
func (e *FunctionExecutor) Stop() error {
	if e.subscriber != nil {
		e.subscriber.Close()
	}
	return nil
}

func (e *FunctionExecutor) handleEvent(evt events.Event) {
	switch evt.Kind {
	case runtime.RegisterFunction:
		// Just try to assert directly to the function signature
		if handler, ok := evt.Data.(func(runtime.Task) (chan *runtime.Result, error)); ok {
			e.handlers.Store(registry.ID(evt.Path), handler)
			e.logger.Debug("function registered", zap.String("function", string(evt.Path)))
			e.bus.Send(e.ctx, events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.AcceptFunctionEvent,
				Path:   evt.Path,
			})
		} else {
			e.logger.Error("invalid handler type",
				zap.String("function", string(evt.Path)),
				zap.String("type", fmt.Sprintf("%T", evt.Data)))

			e.bus.Send(e.ctx, events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RejectFunctionEvent,
				Path:   evt.Path,
			})
		}
	case runtime.DeleteFunction:
		// check if the handler exists before removing it
		_, exists := e.handlers.Load(registry.ID(evt.Path))
		if !exists {
			e.logger.Warn("function not found", zap.String("function", string(evt.Path)))
			e.bus.Send(e.ctx, events.Event{
				System: runtime.FunctionSystem,
				Kind:   runtime.RejectFunctionEvent,
				Path:   evt.Path,
			})
			return
		}

		e.handlers.Delete(registry.ID(evt.Path))
		e.logger.Debug("function removed",
			zap.String("function", string(evt.Path)))
		e.bus.Send(e.ctx, events.Event{
			System: runtime.FunctionSystem,
			Kind:   runtime.AcceptFunctionEvent,
			Path:   evt.Path,
		})
	}
}

// Execute runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (e *FunctionExecutor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := e.handlers.Load(task.Target)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.Target)
	}

	if execHandler, ok := handler.(func(runtime.Task) (chan *runtime.Result, error)); ok {
		return execHandler(task)
	}

	return nil, fmt.Errorf("invalid handler type for target: %s", task.Target)
}
