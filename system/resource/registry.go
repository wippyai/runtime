// SPDX-License-Identifier: MPL-2.0

package resource

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/wippyai/runtime/api/resource"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Registry manages resource registration and access
type Registry struct {
	ctx        context.Context
	bus        event.Bus
	logger     *zap.Logger
	subscriber *eventbus.Subscriber
	resources  map[registry.ID]*resourceSlot
	mu         sync.RWMutex
	stopped    bool
}

type resourceSlot struct {
	active  *resourceGeneration
	retired map[*resourceGeneration]struct{}
}

type resourceGeneration struct {
	entry   resource.Entry
	borrows int
}

// NewRegistry creates a new resource registry instance
func NewRegistry(bus event.Bus, logger *zap.Logger) *Registry {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Registry{
		bus:       bus,
		logger:    logger,
		resources: make(map[registry.ID]*resourceSlot),
	}
}

// Start initializes the service and begins listening for resource events
func (s *Registry) Start(ctx context.Context) error {
	s.ctx = ctx
	s.mu.Lock()
	s.stopped = false
	s.mu.Unlock()

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
		s.subscriber = nil
	}
	s.mu.Lock()
	s.stopped = true
	for id, slot := range s.resources {
		s.retireActiveLocked(slot)
		if len(slot.retired) == 0 {
			delete(s.resources, id)
		}
	}
	s.mu.Unlock()
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
		s.sendReject(e.Path, "invalid resource entry payload")
		return
	}
	outcome := outcomePath(e, entry)
	if isNilInterface(entry.Provider) {
		s.logger.Error("resource entry has nil provider", zap.String("resource", e.Path))
		s.sendReject(outcome, "resource entry has nil provider")
		return
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		s.sendReject(outcome, "resource registry stopped")
		return
	}
	s.activateLocked(entry)
	s.mu.Unlock()
	s.logger.Debug("resource registered",
		zap.String("id", entry.ID.String()),
		zap.Any("meta", entry.Meta))
	s.sendAccept(outcome)
}

func (s *Registry) handleUpdate(e event.Event) {
	entry, ok := e.Data.(resource.Entry)
	if !ok {
		s.logger.Error("invalid resource entry payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		s.sendReject(e.Path, "invalid resource entry payload")
		return
	}
	outcome := outcomePath(e, entry)
	if isNilInterface(entry.Provider) {
		s.logger.Error("resource entry has nil provider", zap.String("resource", e.Path))
		s.sendReject(outcome, "resource entry has nil provider")
		return
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		s.sendReject(outcome, "resource registry stopped")
		return
	}
	slot, exists := s.resources[entry.ID]
	if !exists || slot.active == nil {
		s.mu.Unlock()
		s.logger.Warn("resource not found for update",
			zap.String("id", entry.ID.String()))
		s.sendReject(outcome, "resource not found for update")
		return
	}
	s.activateLocked(entry)
	s.mu.Unlock()
	s.logger.Debug("resource updated",
		zap.String("id", entry.ID.String()),
		zap.Any("meta", entry.Meta))
	s.sendAccept(outcome)
}

func (s *Registry) handleRemove(e event.Event) {
	id, ok := e.Data.(registry.ID)
	if !ok {
		s.logger.Error("invalid resource ID payload",
			zap.String("resource", e.Path),
			zap.String("type", fmt.Sprintf("%T", e.Data)))
		s.sendReject(e.Path, "invalid resource ID payload")
		return
	}

	s.mu.Lock()
	slot, exists := s.resources[id]
	if !exists {
		s.mu.Unlock()
		s.logger.Warn("resource not found for removal",
			zap.String("id", id.String()))
		s.sendReject(e.Path, "resource not found for removal")
		return
	}
	s.retireActiveLocked(slot)
	if len(slot.retired) == 0 {
		delete(s.resources, id)
	}
	s.mu.Unlock()
	s.logger.Debug("resource removed",
		zap.String("id", id.String()))
	s.sendAccept(e.Path)
}

// outcomePath is where the outcome of an operation is published: the caller's
// own operation id when it supplied one, so a reply can only satisfy the
// operation that asked for it, and the resource path otherwise.
func outcomePath(e event.Event, entry resource.Entry) event.Path {
	if entry.OpID != "" {
		return entry.OpID
	}
	return e.Path
}

// sendAccept reports that the registry applied an operation. It is published
// after the registry state is visible to Acquire, so a caller awaiting it knows
// the new provider is the one being served.
func (s *Registry) sendAccept(path event.Path) {
	s.publish(event.Event{
		System: resource.System,
		Kind:   resource.Accept,
		Path:   path,
	})
}

// sendReject reports that the registry declined an operation, so a caller does
// not wait out its timeout for an outcome that will never arrive.
func (s *Registry) sendReject(path event.Path, reason string) {
	s.publish(event.Event{
		System: resource.System,
		Kind:   resource.Reject,
		Path:   path,
		Data:   reason,
	})
}

func (s *Registry) publish(e event.Event) {
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}
	s.bus.Send(ctx, e)
}

// Acquire attempts to acquire a resource with the specified access mode
func (s *Registry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.Lock()
	slot, ok := s.resources[id]
	if s.stopped || !ok || slot.active == nil {
		s.mu.Unlock()
		return nil, resource.ErrNotFound
	}
	generation := slot.active
	entry := generation.entry
	generation.borrows++
	s.mu.Unlock()

	res, err := entry.Provider.Acquire(ctx, id, mode)
	if err != nil {
		s.releaseBorrow(id, generation)
		return nil, err
	}
	if isNilInterface(res) {
		s.releaseBorrow(id, generation)
		return nil, ErrInvalidResource
	}

	return resource.NewTrackedResource(res, func() {
		s.releaseBorrow(id, generation)
	}), nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// activateLocked publishes a new acquisition generation. Older generations
// remain reachable only by the resources that already borrowed them.
func (s *Registry) activateLocked(entry resource.Entry) {
	slot := s.resources[entry.ID]
	if slot == nil {
		slot = &resourceSlot{}
		s.resources[entry.ID] = slot
	}
	s.retireActiveLocked(slot)
	slot.active = &resourceGeneration{entry: entry}
}

func (s *Registry) retireActiveLocked(slot *resourceSlot) {
	if slot.active == nil {
		return
	}
	if slot.active.borrows > 0 {
		if slot.retired == nil {
			slot.retired = make(map[*resourceGeneration]struct{})
		}
		slot.retired[slot.active] = struct{}{}
	}
	slot.active = nil
}

func (s *Registry) releaseBorrow(id registry.ID, generation *resourceGeneration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	slot, exists := s.resources[id]
	if !exists || generation.borrows == 0 {
		return
	}
	if slot.active != generation {
		if _, retired := slot.retired[generation]; !retired {
			return
		}
	}
	generation.borrows--
	if generation.borrows != 0 || slot.active == generation {
		return
	}
	delete(slot.retired, generation)
	if slot.active == nil && len(slot.retired) == 0 {
		delete(s.resources, id)
	}
}

// List returns all registered resource IDs
func (s *Registry) List() ([]registry.ID, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	resources := make([]registry.ID, 0, len(s.resources))
	for id, slot := range s.resources {
		if slot.active != nil {
			resources = append(resources, id)
		}
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].String() < resources[j].String()
	})
	return resources, nil
}

// Exists checks if a resource is registered
func (s *Registry) Exists(id registry.ID) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	slot, exists := s.resources[id]
	return exists && slot.active != nil
}

// Implementation of Registry interface
var _ resource.Registry = (*Registry)(nil)
