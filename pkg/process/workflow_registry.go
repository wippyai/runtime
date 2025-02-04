package process

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/process"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// Registry manages workflow handlers and their registration in the runtime system.
// It uses an event bus for communication and supports dynamic handler registration.
type Registry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	handlers   sync.Map
	subscriber *eventbus.Subscriber
}

// NewRegistry creates a new Registry instance with the provided event bus and logger.
func NewRegistry(bus events.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		bus:    bus,
		logger: logger,
	}
}

// Start initializes the registry and begins listening for workflow events.
// It sets up a subscriber for handling workflow-related events on the event bus.
func (r *Registry) Start(ctx context.Context) error {
	r.ctx = ctx

	// Subscribe to workflow events
	sub, err := eventbus.NewSubscriber(
		r.ctx,
		r.bus,
		process.WorkflowSystem,
		"workflow.*",
		r.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	r.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry by closing its event subscriber.
func (r *Registry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

// handleEvent processes incoming workflow events for handler registration and deletion.
func (r *Registry) handleEvent(evt events.Event) {
	switch evt.Kind {
	case process.RegisterWorkflowEvent:
		if data, ok := evt.Data.(process.RegisterWorkflow); ok {
			if data.Handler == nil {
				r.logger.Warn("handler is nil", zap.String("target", string(data.Target)))
				return
			}
			r.handlers.Store(data.Target, data.Handler)
			r.logger.Info("workflow handler registered",
				zap.String("target", string(data.Target)))
		}
	case process.DeleteWorkflowEvent:
		if data, ok := evt.Data.(process.DeleteWorkflow); ok {
			r.handlers.Delete(data.Target)
			r.logger.Info("workflow handler removed",
				zap.String("target", string(data.Target)))
		}
	}
}

// Get retrieves a registered workflow handler for the given target ID.
// Returns an error if no handler is registered for the target.
func (r *Registry) Get(id registry.ID) (func() any, error) {
	handler, exists := r.handlers.Load(id)
	if !exists {
		return nil, fmt.Errorf("no workflow handler registered for target: %s", id)
	}

	return handler.(func() any), nil
}

// Ensure Registry implements WorkflowRegistry interface
var _ process.WorkflowRegistry = (*Registry)(nil)
