package executor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Executor manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type Executor struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

// NewExecutor creates a new Executor instance with the provided event bus and logger.
func NewExecutor(bus events.Bus, logger *zap.Logger) *Executor {
	return &Executor{
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the executor and begins listening for executor events.
// It sets up a subscriber for handling executor-related events on the event bus.
func (e *Executor) Start(ctx context.Context) error {
	e.ctx = ctx

	// Subscribe to executor events
	sub, err := eventbus.NewSubscriber(
		e.ctx,
		e.bus,
		runtime.System,
		"executor.*",
		e.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	e.subscriber = sub

	return nil
}

// Stop cleanly shuts down the executor by closing its event subscriber.
func (e *Executor) Stop() error {
	if e.subscriber != nil {
		e.subscriber.Close()
	}
	return nil
}

func (e *Executor) handleEvent(evt events.Event) {
	switch evt.Kind {
	case runtime.RegisterFunctionEvent:
		if data, ok := evt.Data.(runtime.Function); ok {
			e.handlers.Store(evt.Path, data)
			e.logger.Debug("function registered",
				zap.String("function", string(evt.Path)))
		}
	case runtime.DeleteFunctionEvent:
		e.handlers.Delete(evt.Path)
		e.logger.Debug("function removed",
			zap.String("function", string(evt.Path)))
	}
}

// Execute runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (e *Executor) Execute(task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := e.handlers.Load(task.Target)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.Target)
	}

	if execHandler, ok := handler.(runtime.Function); ok {
		return execHandler(task)
	}

	return nil, fmt.Errorf("invalid handler type for target: %s", task.Target)
}
