package noop

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

// Runtime implements a no-op runtime manager that satisfies the EntryListener interface
type Runtime struct {
	bus    events.Bus
	logger *zap.Logger
}

// NewNoopRuntime creates a new instance of Runtime
func NewNoopRuntime(bus events.Bus, logger *zap.Logger) *Runtime {
	return &Runtime{
		bus:    bus,
		logger: logger,
	}
}

// Execute implements the runtime.Registry interface, does not do anything.
func (n *Runtime) Execute(ctx context.Context, task runtime.Task) (chan *runtime.Result, error) {
	rspChan := make(chan *runtime.Result, 1)
	rspChan <- &runtime.Result{
		Payload: payload.New(fmt.Sprintf("noop runtime: task %s executed", task.Handler)),
	}
	return rspChan, nil
}

// Add implements EntryListener.Add - does nothing and returns nil
func (n *Runtime) Add(ctx context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: add called",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	n.bus.Send(ctx, events.Event{
		System: function.System,
		Kind:   function.RegisterFunctionHandler,
		Path:   entry.ID.String(),
		Data:   function.Func(n.Execute),
	})

	return nil
}

// Update implements EntryListener.Update - does nothing and returns nil
func (n *Runtime) Update(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: update called",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))
	return nil
}

// Delete implements EntryListener.Delete - does nothing and returns nil
func (n *Runtime) Delete(ctx context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: delete called",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	n.bus.Send(ctx, events.Event{
		System: function.System,
		Kind:   function.DeleteFunctionHandler,
		Path:   entry.ID.String(),
	})

	return nil
}
