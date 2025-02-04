package noop

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
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

// Execute implements the runtime.Executor interface, does not do anything.
func (n *Runtime) Execute(task runtime.Task) (chan *runtime.Result, error) {
	rspChan := make(chan *runtime.Result, 1)
	rspChan <- &runtime.Result{
		Payload: payload.New(fmt.Sprintf("noop runtime: task %s executed", task.Target)),
	}

	return rspChan, nil
}

// Add implements EntryListener.Add - does nothing and returns nil
func (n *Runtime) Add(ctx context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: add called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))

	n.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.RegisterHandlerEvent,
		Data:   runtime.RegisterHandler{Target: entry.ID, Handler: n.Execute},
	})

	return nil
}

// Update implements EntryListener.Update - does nothing and returns nil
func (n *Runtime) Update(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: update called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))
	return nil
}

// Delete implements EntryListener.Delete - does nothing and returns nil
func (n *Runtime) Delete(ctx context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: delete called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))

	n.bus.Send(ctx, events.Event{
		System: runtime.System,
		Kind:   runtime.DeleteHandlerEvent,
		Data:   runtime.DeleteHandler{Target: entry.ID},
	})

	return nil
}
