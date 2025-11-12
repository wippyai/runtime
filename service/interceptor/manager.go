package interceptor

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/interceptor"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Manager handles interceptor lifecycle and resource provisioning
type Manager struct {
	logger   *zap.Logger
	eventBus event.Bus
}

// NewManager creates a new interceptor manager
func NewManager(eventBus event.Bus, logger *zap.Logger) *Manager {
	return &Manager{
		logger:   logger,
		eventBus: eventBus,
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	ic, ok := entry.Data.Data().(interceptor.Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	// Register as registry storage
	m.eventBus.Send(ctx, event.Event{
		System: interceptor.System,
		Kind:   interceptor.Register,
		Path:   entry.ID.String(),
		Data:   ic,
	})

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	ic, ok := entry.Data.Data().(interceptor.Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	m.eventBus.Send(ctx, event.Event{
		System: interceptor.System,
		Kind:   interceptor.Update,
		Path:   entry.ID.String(),
		Data:   ic,
	})

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.eventBus.Send(ctx, event.Event{
		System: interceptor.System,
		Kind:   interceptor.Delete,
		Path:   entry.ID.String(),
	})

	return nil
}
