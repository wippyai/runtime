package noop

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

// NoopRuntime implements a no-op runtime manager that satisfies the EntryListener interface
type NoopRuntime struct {
	bus    events.Bus
	logger *zap.Logger
}

// NewNoopRuntime creates a new instance of NoopRuntime
func NewNoopRuntime(bus events.Bus, logger *zap.Logger) *NoopRuntime {
	return &NoopRuntime{
		bus:    bus,
		logger: logger,
	}
}

func (n *NoopRuntime) Execute(task runtime.Task) (chan *runtime.Result, error) {
	rspChan := make(chan *runtime.Result, 1)
	rspChan <- &runtime.Result{
		Payload: payload.New(fmt.Sprintf("noop runtime: task %s executed", task.Target)),
	}

	return rspChan, nil
}

// Add implements EntryListener.Add - does nothing and returns nil
func (n *NoopRuntime) Add(ctx context.Context, entry registry.Entry) error {
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
func (n *NoopRuntime) Update(_ context.Context, entry registry.Entry) error {
	n.logger.Debug("noop runtime: update called",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(entry.Kind)))
	return nil
}

// Delete implements EntryListener.Delete - does nothing and returns nil
func (n *NoopRuntime) Delete(ctx context.Context, entry registry.Entry) error {
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
