package resource

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/resource"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages resource registration and access
type Registry struct {
	ctx        context.Context
	logger     *zap.Logger
	bus        events.Bus
	resources  sync.Map // map[registry.ID]Entry
	subscriber *eventbus.Subscriber
}

// NewResourceRegistry creates a new resource service instance
func NewResourceRegistry(bus events.Bus, logger *zap.Logger) *Registry {
	return &Registry{
		bus:       bus,
		logger:    logger,
		resources: sync.Map{},
	}
}

// Start initializes the service and begins listening for resource events
func (s *Registry) Start(ctx context.Context) error {
	s.ctx = ctx

	// Subscribe to resource events
	sub, err := eventbus.NewSubscriber(
		s.ctx,
		s.bus,
		resource.System,
		"resources.(register|update|remove)",
		s.handleEvent,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.subscriber = sub

	return nil
}

// Stop cleanly shuts down the service
func (s *Registry) Stop() error {
	if s.subscriber != nil {
		s.subscriber.Close()
	}
	return nil
}

func (s *Registry) handleEvent(e events.Event) {
	switch e.Kind {
	case resource.Register:
		s.handleRegister(e)
	case resource.Update:
		s.handleUpdate(e)
	case resource.Remove:
		s.handleRemove(e)
	default:
		s.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (s *Registry) handleRegister(e events.Event) {
	entry, ok := e.Data.(resource.Entry)
	if !ok {
		s.logger.Error("invalid resource entry payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	// Store the resource entry
	s.resources.Store(entry.ID, entry)
	s.logger.Debug("resource registered",
		zap.String("id", entry.ID.String()),
		zap.Any("meta", entry.Meta))
}

func (s *Registry) handleUpdate(e events.Event) {
	entry, ok := e.Data.(resource.Entry)
	if !ok {
		s.logger.Error("invalid resource entry payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	// Update existing resource
	if _, exists := s.resources.Load(entry.ID); !exists {
		s.logger.Warn("resource not found for update",
			zap.String("id", entry.ID.String()))
		return
	}

	s.resources.Store(entry.ID, entry)
	s.logger.Debug("resource updated",
		zap.String("id", entry.ID.String()),
		zap.Any("meta", entry.Meta))
}

func (s *Registry) handleRemove(e events.Event) {
	id, ok := e.Data.(registry.ID)
	if !ok {
		s.logger.Error("invalid resource ID payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	// Remove the resource
	if _, exists := s.resources.Load(id); !exists {
		s.logger.Warn("resource not found for removal",
			zap.String("id", id.String()))
		return
	}

	s.resources.Delete(id)
	s.logger.Debug("resource removed",
		zap.String("id", id.String()))
}

// Acquire attempts to acqu,m.8klij ire a resource with the specified access mode
func (s *Registry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	entryVal, ok := s.resources.Load(id)
	if !ok {
		return nil, resource.ErrResourceNotFound
	}

	entry := entryVal.(resource.Entry)
	return entry.Provider.Acquire(ctx, id, mode)
}

// List returns all registered resource IDs
func (s *Registry) List() ([]registry.ID, error) {
	var resources []registry.ID
	s.resources.Range(func(key, _ interface{}) bool {
		resources = append(resources, key.(registry.ID))
		return true
	})
	return resources, nil
}

// Exists checks if a resource is registered
func (s *Registry) Exists(id registry.ID) bool {
	_, exists := s.resources.Load(id)
	return exists
}

// Implementation of Registry interface
var _ resource.Registry = (*Registry)(nil)
