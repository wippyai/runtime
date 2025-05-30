package interceptor

import (
	"context"
	"fmt"

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
	interceptor, ok := entry.Data.(Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	// Send register event to the registry
	m.eventBus.Send(ctx, event.Event{
		System: System,
		Kind:   Register,
		Path:   entry.ID.String(),
		Data:   interceptor,
	})

	m.logger.Info("sent interceptor registration request",
		zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(_ context.Context, entry registry.Entry) error {
	interceptor, ok := entry.Data.(Interceptor)
	if !ok {
		return fmt.Errorf("invalid interceptor data type")
	}

	// Send register event to the registry (same as Add since we don't distinguish)
	m.eventBus.Send(context.Background(), event.Event{
		System: System,
		Kind:   Update,
		Path:   fmt.Sprintf("%s/%s", entry.ID.NS, entry.ID.Name),
		Data:   interceptor,
	})

	m.logger.Info("sent interceptor update request",
		zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(_ context.Context, entry registry.Entry) error {
	// Send delete event to the registry
	m.eventBus.Send(context.Background(), event.Event{
		System: System,
		Kind:   Delete,
		Path:   fmt.Sprintf("%s/%s", entry.ID.NS, entry.ID.Name),
	})

	m.logger.Info("sent interceptor deletion request",
		zap.String("id", entry.ID.String()))
	return nil
}
