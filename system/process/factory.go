package process

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// factoryEntry stores factory and its metadata.
type factoryEntry struct {
	factory api.ProcessFactory
	meta    api.ProcessMeta
}

// FactoryRegistry manages process factories.
// Uses event bus for registration during load phase.
type FactoryRegistry struct {
	ctx        context.Context
	log        *zap.Logger
	bus        event.Bus
	factories  sync.Map // registry.ID -> *factoryEntry
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

	sub, err := eventbus.NewSubscriber(
		ctx,
		r.bus,
		api.FactorySystem,
		"factory.(register|delete)",
		r.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
	}
	r.subscriber = sub

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
		r.registerFactory(e)
	case api.FactoryDelete:
		r.deleteFactory(e)
	default:
		r.log.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (r *FactoryRegistry) registerFactory(e event.Event) {
	entry, ok := e.Data.(*api.FactoryEntry)
	if !ok {
		r.log.Error("invalid factory entry",
			zap.String("path", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		r.sendReject(e.Path, "invalid factory entry")
		return
	}

	id := registry.ParseID(e.Path)
	r.factories.Store(id, &factoryEntry{
		factory: entry.Factory,
		meta:    entry.Meta,
	})
	r.log.Debug("factory registered", zap.String("id", id.String()))
	r.sendAccept(e.Path)
}

func (r *FactoryRegistry) deleteFactory(e event.Event) {
	id := registry.ParseID(e.Path)

	_, exists := r.factories.Load(id)
	if !exists {
		r.log.Warn("factory not found",
			zap.String("path", e.Path),
			zap.String("ns", id.NS),
			zap.String("name", id.Name))
		r.sendReject(e.Path, "factory not found")
		return
	}

	r.factories.Delete(id)
	r.log.Debug("factory deleted",
		zap.String("path", e.Path),
		zap.String("ns", id.NS),
		zap.String("name", id.Name))
	r.sendAccept(e.Path)
}

func (r *FactoryRegistry) sendAccept(path event.Path) {
	r.bus.Send(r.ctx, event.Event{
		System: api.FactorySystem,
		Kind:   api.FactoryAccept,
		Path:   path,
	})
}

func (r *FactoryRegistry) sendReject(path event.Path, reason string) {
	r.bus.Send(r.ctx, event.Event{
		System: api.FactorySystem,
		Kind:   api.FactoryReject,
		Path:   path,
		Data:   reason,
	})
}

// Create implements process.Factory.
func (r *FactoryRegistry) Create(id registry.ID) (api.Process, *api.ProcessMeta, error) {
	val, ok := r.factories.Load(id)
	if !ok {
		return nil, nil, NewFactoryNotFoundError(id)
	}

	entry, ok := val.(*factoryEntry)
	if !ok {
		return nil, nil, NewInvalidFactoryEntryError(id)
	}

	proc, err := entry.factory()
	if err != nil {
		return nil, nil, NewProcessCreateError(err)
	}

	r.log.Debug("process created",
		zap.String("ns", id.NS),
		zap.String("name", id.Name))

	return proc, &entry.meta, nil
}

// Has checks if a factory is registered for the given ID.
func (r *FactoryRegistry) Has(id registry.ID) bool {
	_, ok := r.factories.Load(id)
	return ok
}
