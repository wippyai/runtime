package policy

import (
	"context"

	policyapi "github.com/wippyai/runtime/api/service/security/policy"

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
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:     logger,
		bus:     bus,
		factory: factory,
	}
}

// Add handles the registration of a new security policy
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	return m.processPolicy(ctx, entry, security.PolicyRegister, "registered")
}

// Update handles the update of an existing security policy
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return m.processPolicy(ctx, entry, security.PolicyUpdate, "updated")
}

// Delete handles the removal of a security policy
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if !m.isSupportedKind(entry.Kind) {
		return NewUnsupportedEntryKindError(entry.Kind)
	}

	m.bus.Send(ctx, event.Event{
		System: security.System,
		Kind:   security.PolicyDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("security policy removed", zap.String("id", entry.ID.String()))

	return nil
}

// processPolicy is a helper that handles common logic for Add and Update operations
func (m *Manager) processPolicy(ctx context.Context, entry registry.Entry, eventKind event.Kind, action string) error {
	if !m.isSupportedKind(entry.Kind) {
		return NewUnsupportedEntryKindError(entry.Kind)
	}

	policyEntry, err := m.factory.CreatePolicyEntry(ctx, entry)
	if err != nil {
		return NewCreatePolicyEntryError(err)
	}

	m.bus.Send(ctx, event.Event{
		System: security.System,
		Kind:   eventKind,
		Path:   entry.ID.String(),
		Data:   policyEntry,
	})

	m.log.Info("security policy "+action,
		zap.String("id", entry.ID.String()),
		zap.Int("groups", len(policyEntry.Groups)))

	return nil
}

// isSupportedKind checks if the entry kind is a supported policy kind
func (m *Manager) isSupportedKind(kind registry.Kind) bool {
	return kind == policyapi.Policy || kind == policyapi.ExprKind
}
