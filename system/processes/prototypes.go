package processes

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/process"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// PrototypeRegistry manages process prototypes and handles process creation in the runtime system.
// It uses an event bus for communication and supports dynamic prototype registration.
type PrototypeRegistry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	prototypes sync.Map
	subscriber *eventbus.Subscriber
}

// NewProcessFactory creates a new PrototypeRegistry instance with the provided event bus and logger.
func NewProcessFactory(bus events.Bus, logger *zap.Logger) *PrototypeRegistry {
	return &PrototypeRegistry{
		bus:        bus,
		logger:     logger,
		prototypes: sync.Map{},
	}
}

// Start initializes the registry and begins listening for process-related events.
func (p *PrototypeRegistry) Start(ctx context.Context) error {
	p.ctx = ctx

	// Subscribe to process events
	sub, err := eventbus.NewSubscriber(
		p.ctx,
		p.bus,
		process.PrototypeSystem,
		"prototype.(register|remove)",
		p.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	p.subscriber = sub

	return nil
}

// Stop cleanly shuts down the registry by closing its event subscriber.
func (p *PrototypeRegistry) Stop() error {
	if p.subscriber != nil {
		p.subscriber.Close()
	}
	return nil
}

func (p *PrototypeRegistry) handleEvent(e events.Event) {
	switch e.Kind {
	case process.RegisterPrototype:
		p.registerPrototype(e)
	case process.DeletePrototype:
		p.deletePrototype(e)
	default:
		p.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (p *PrototypeRegistry) registerPrototype(e events.Event) {
	prototype, ok := e.Data.(process.Prototype)
	if !ok {
		p.logger.Error("invalid register prototype payload",
			zap.String("process", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))

		p.sendReject(e.Path, "invalid register prototype payload")
		return
	}

	// Store the prototype
	p.prototypes.Store(registry.ParseID(e.Path), prototype)
	p.logger.Debug("prototype registered", zap.String("process", e.Path))

	p.sendAccept(e.Path)
}

func (p *PrototypeRegistry) deletePrototype(e events.Event) {
	id := registry.ParseID(e.Path)
	// Check if the prototype exists before removing
	_, exists := p.prototypes.Load(id)
	if !exists {
		p.logger.Warn("prototype not found",
			zap.String("process", e.Path),
			zap.String("ns", id.NS),
			zap.String("name", id.Name))
		p.sendReject(e.Path, "prototype not found")
		return
	}

	// Remove the prototype
	p.prototypes.Delete(id)
	p.logger.Debug("prototype removed",
		zap.String("process", e.Path),
		zap.String("ns", id.NS),
		zap.String("name", id.Name))

	p.sendAccept(e.Path)
}

func (p *PrototypeRegistry) sendAccept(path events.Path) {
	p.bus.Send(p.ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.AcceptPrototype,
		Path:   path,
	})
}

func (p *PrototypeRegistry) sendReject(path events.Path, reason string) {
	p.bus.Send(p.ctx, events.Event{
		System: process.PrototypeSystem,
		Kind:   process.RejectPrototype,
		Path:   path,
		Data:   reason,
	})
}

// Create instantiates a new process using the registered prototype for the given ID.
// Returns an error if no prototype is registered for the ID or if process creation fails.
func (p *PrototypeRegistry) Create(id registry.ID) (process.Process, error) {
	prototypeVal, exists := p.prototypes.Load(id)
	if !exists {
		return nil, fmt.Errorf("no prototype registered for id: %v", id)
	}

	prototype, ok := prototypeVal.(process.Prototype)
	if !ok {
		return nil, fmt.Errorf("invalid prototype type for id: %v", id)
	}

	proto, err := prototype()
	if err != nil {
		return nil, fmt.Errorf("failed to create process from prototype: %w", err)
	}

	p.logger.Debug("process created",
		zap.String("ns", id.NS),
		zap.String("name", id.Name))

	return proto, nil
}
