package resource

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/resource"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Note: fmt kept for Sprintf in logging

// Registry manages resource registration and access
type Registry struct {
	ctx         context.Context
	logger      *zap.Logger
	bus         event.Bus
	resources   sync.Map // map[registry.ID]resource.Entry
	borrowCount sync.Map // map[registry.ID]*atomic.Int32
	subscriber  *eventbus.Subscriber
}

// NewResourceRegistry creates a new resource service instance
func NewResourceRegistry(bus event.Bus, logger *zap.Logger) *Registry {
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
		"resource.(register|update|delete)",
		s.handleEvent,
	)
	if err != nil {
		return NewSubscriberError(err)
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

func (s *Registry) handleEvent(e event.Event) {
	switch e.Kind {
	case resource.Register:
		s.handleRegister(e)
	case resource.Update:
		s.handleUpdate(e)
	case resource.Delete:
		s.handleRemove(e)
	default:
		s.logger.Warn("unknown event kind",
			zap.String("kind", e.Kind),
			zap.String("path", e.Path))
	}
}

func (s *Registry) handleRegister(e event.Event) {
	entry, ok := e.Data.(resource.Entry)
	if !ok {
		s.logger.Error("invalid resource entry payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	// Store the resource entry and initialize borrow counter
	s.resources.Store(entry.ID, entry)
	s.borrowCount.Store(entry.ID, new(atomic.Int32))
	s.logger.Debug("resource registered",
		zap.String("id", entry.ID.String()),
		zap.Any("meta", entry.Meta))
}

func (s *Registry) handleUpdate(e event.Event) {
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

func (s *Registry) handleRemove(e event.Event) {
	id, ok := e.Data.(registry.ID)
	if !ok {
		s.logger.Error("invalid resource Source payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		return
	}

	// Check if resource exists
	if _, exists := s.resources.Load(id); !exists {
		s.logger.Warn("resource not found for removal",
			zap.String("id", id.String()))
		return
	}

	// Check if resource is borrowed
	if countVal, ok := s.borrowCount.Load(id); ok {
		count := countVal.(*atomic.Int32).Load()
		if count > 0 {
			s.logger.Warn("cannot delete borrowed resource",
				zap.String("id", id.String()),
				zap.Int32("borrows", count))
			return
		}
	}

	s.resources.Delete(id)
	s.borrowCount.Delete(id)
	s.logger.Debug("resource removed",
		zap.String("id", id.String()))
}

// Acquire attempts to acquire a resource with the specified access mode
func (s *Registry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	entryVal, ok := s.resources.Load(id)
	if !ok {
		return nil, resource.ErrResourceNotFound
	}

	entry := entryVal.(resource.Entry)

	// Get or create borrow counter
	countVal, _ := s.borrowCount.LoadOrStore(id, new(atomic.Int32))
	counter := countVal.(*atomic.Int32)
	counter.Add(1)

	res, err := entry.Provider.Acquire(ctx, id, mode)
	if err != nil {
		counter.Add(-1)
		return nil, err
	}

	// Wrap with tracking - decrement count on release
	return resource.NewTrackedResource(res, func() {
		counter.Add(-1)
	}), nil
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
