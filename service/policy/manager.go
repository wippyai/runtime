package policy

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/service/policy"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
	"go.uber.org/zap"
)

// Manager handles security policy registration and lifecycle
type Manager struct {
	log     *zap.Logger
	bus     event.Bus
	factory FactoryAPI
}

// NewManager creates a new policy manager
func NewManager(
	bus event.Bus,
	factory FactoryAPI,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		log:     logger,
		bus:     bus,
		factory: factory,
	}
}

// Add handles the registration of a new security policy
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != policy.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Create policy entry using factory
	policyEntry, err := m.factory.CreatePolicyEntry(ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to create policy entry: %w", err)
	}

	// Notify through the event bus that a policy has been registered
	m.bus.Send(ctx, event.Event{
		System: security.System,
		Kind:   security.PolicyRegister,
		Path:   entry.ID.String(),
		Data:   policyEntry,
	})

	m.log.Info("security policy registered",
		zap.String("id", entry.ID.String()),
		zap.Int("groups", len(policyEntry.Groups)))

	return nil
}

// Update handles the update of an existing security policy
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != policy.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Create updated policy entry using factory
	policyEntry, err := m.factory.CreatePolicyEntry(ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to create policy entry: %w", err)
	}

	// Notify through the event bus that a policy has been updated
	m.bus.Send(ctx, event.Event{
		System: security.System,
		Kind:   security.PolicyUpdate,
		Path:   entry.ID.String(),
		Data:   policyEntry,
	})

	m.log.Info("security policy updated",
		zap.String("id", entry.ID.String()),
		zap.Int("groups", len(policyEntry.Groups)))

	return nil
}

// Delete handles the removal of a security policy
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != policy.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	// Notify through the event bus that a policy has been deleted
	m.bus.Send(ctx, event.Event{
		System: security.System,
		Kind:   security.PolicyDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("security policy removed", zap.String("id", entry.ID.String()))

	return nil
}
