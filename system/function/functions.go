package function

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/internal/uniqid"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages the execution of tasks by registered handlers in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type Registry struct {
	ctx        context.Context
	host       pubsub.Host
	uniqID     *uniqid.Generator
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

// NewFunctionRegistry creates a new Registry instance with the provided event bus and logger.
func NewFunctionRegistry(bus events.Bus, host pubsub.Host, logger *zap.Logger) *Registry {

	return &Registry{
		uniqID:   uniqid.NewGenerator(),
		bus:      bus,
		host:     host,
		logger:   logger,
		handlers: sync.Map{},
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
		return fmt.Errorf("failed to create subscriber: %w", err)
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

func (f *Registry) handleEvent(e events.Event) {
	switch e.Kind {
	case function.FuncRegister:
		f.registerFunction(e)
	case function.FuncDelete:
		f.deleteFunction(e)
	default:
		f.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (f *Registry) registerFunction(e events.Event) {
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

func (f *Registry) deleteFunction(e events.Event) {
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

func (f *Registry) sendAccept(path events.Path) {
	f.bus.Send(f.ctx, events.Event{
		System: function.System,
		Kind:   function.FuncAccept,
		Path:   path,
	})
}

func (f *Registry) sendReject(path events.Path, reason string) {
	f.bus.Send(f.ctx, events.Event{
		System: function.System,
		Kind:   function.FuncReject,
		Path:   path,
		Data:   reason,
	})
}

// Call runs the given task using its registered handler and returns a channel
// for receiving the execution result(s). Returns an error if no handler is registered
// for the task's target or if the handler type is invalid.
func (f *Registry) Call(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	handler, exists := f.handlers.Load(task.ID)
	if !exists {
		return nil, fmt.Errorf("no handler registered for target: %s", task.ID)
	}

	// keep context boundaries
	if ctx == nil {
		ctx = context.Background()
	}

	execHandler, ok := handler.(function.Func)
	if !ok {
		return nil, fmt.Errorf("invalid handler type for target: %s", task.ID)
	}

	ctx = function.WithContext(ctx, &function.Context{
		PID: pubsub.PID{
			Node:   pubsub.GetNode(ctx).ID(),
			Host:   function.HostID,
			ID:     task.ID,
			UniqID: f.uniqID.Generate(),
		},
	})
	ctx = pubsub.WithHost(ctx, f.host)

	return execHandler(ctx, task)
}
