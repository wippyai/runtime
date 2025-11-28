package process2

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// FactoryRegistry manages scheduler process factories.
// Providers register factories by ID, consumers create processes by ID.
type FactoryRegistry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	factories  sync.Map // registry.ID -> scheduler.ProcessFactory
	subscriber *eventbus.Subscriber
}

// NewFactoryRegistry creates a new factory registry.
func NewFactoryRegistry(bus event.Bus, log *zap.Logger) *FactoryRegistry {
	return &FactoryRegistry{
		bus: bus,
		log: log,
	}
}

// Start initializes the registry and subscribes to factory events.
func (r *FactoryRegistry) Start(ctx context.Context) error {
	r.ctx = ctx

	if r.bus != nil {
		sub, err := eventbus.NewSubscriber(
			ctx,
			r.bus,
			api.FactorySystem,
			"factory.(register|delete)",
			r.handleEvent,
		)
		if err != nil {
			return fmt.Errorf("failed to create subscriber: %w", err)
		}
		r.subscriber = sub
	}

	return nil
}

// Stop shuts down the registry.
func (r *FactoryRegistry) Stop() error {
	if r.subscriber != nil {
		r.subscriber.Close()
	}
	return nil
}

func (r *FactoryRegistry) handleEvent(e event.Event) {
	switch e.Kind {
	case api.FactoryRegister:
		r.registerFromEvent(e)
	case api.FactoryDelete:
		r.deleteFromEvent(e)
	}
}

func (r *FactoryRegistry) registerFromEvent(e event.Event) {
	entry, ok := e.Data.(*api.FactoryEntry)
	if !ok {
		r.log.Error("invalid factory entry", zap.String("path", e.Path))
		return
	}

	id := registry.ParseID(e.Path)
	r.factories.Store(id, entry.Factory)
	r.log.Debug("factory registered", zap.String("id", id.String()))
}

func (r *FactoryRegistry) deleteFromEvent(e event.Event) {
	id := registry.ParseID(e.Path)
	r.factories.Delete(id)
	r.log.Debug("factory deleted", zap.String("id", id.String()))
}

// Register directly registers a factory.
func (r *FactoryRegistry) Register(id registry.ID, factory api.ProcessFactory) {
	r.factories.Store(id, factory)
	if r.log != nil {
		r.log.Debug("factory registered", zap.String("id", id.String()))
	}
}

// Delete directly removes a factory.
func (r *FactoryRegistry) Delete(id registry.ID) {
	r.factories.Delete(id)
	if r.log != nil {
		r.log.Debug("factory deleted", zap.String("id", id.String()))
	}
}

// Create implements process2.Factory.
func (r *FactoryRegistry) Create(id registry.ID) (api.Process, error) {
	val, ok := r.factories.Load(id)
	if !ok {
		return nil, fmt.Errorf("no factory registered for: %s", id)
	}

	factory := val.(api.ProcessFactory)
	return factory()
}

// Has checks if a factory is registered for the given ID.
func (r *FactoryRegistry) Has(id registry.ID) bool {
	_, ok := r.factories.Load(id)
	return ok
}

// GetFactory returns the ProcessFactory for the given ID, or nil if not found.
func (r *FactoryRegistry) GetFactory(id registry.ID) api.ProcessFactory {
	val, ok := r.factories.Load(id)
	if !ok {
		return nil
	}
	return val.(api.ProcessFactory)
}
