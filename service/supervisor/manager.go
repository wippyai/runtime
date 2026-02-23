// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles process service lifecycle and registration.
// It listens to registry for process.service entries and registers
// them with the supervisor system.
type Manager struct {
	log      *zap.Logger
	bus      event.Bus
	dtt      payload.Transcoder
	pidGen   process.PIDGenerator
	services sync.Map // map[registry.ID]*Service
}

// NewManager creates a new process supervisor manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	pidGen process.PIDGenerator,
	logger *zap.Logger,
) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:    logger,
		bus:    bus,
		dtt:    dtt,
		pidGen: pidGen,
	}
}

func (m *Manager) validateEntryKind(entry registry.Entry) error {
	if entry.Kind != supervisorapi.ProcessService {
		return newInvalidEntryKindError(entry.Kind, supervisorapi.ProcessService)
	}
	return nil
}

// Add implements registry.EntryListener.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if err := m.validateEntryKind(entry); err != nil {
		return err
	}

	cfg, err := entryutil.DecodeEntryConfig[supervisorapi.ServiceConfig](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	svc := NewService(entry.ID, *cfg, m.pidGen)
	m.services.Store(entry.ID, svc)

	// Register with supervisor system
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: svc,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Debug("process service added", zap.String("id", entry.ID.String()))
	return nil
}

// Update implements registry.EntryListener.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if err := m.validateEntryKind(entry); err != nil {
		return err
	}

	svc, exists := m.services.Load(entry.ID)
	if !exists {
		return newServiceNotFoundError(entry.ID.String())
	}

	cfg, err := entryutil.DecodeEntryConfig[supervisorapi.ServiceConfig](ctx, m.dtt, entry)
	if err != nil {
		return newDecodeConfigError(err)
	}

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	// Update stored service config
	svc.(*Service).config = *cfg

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Debug("process service updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete implements registry.EntryListener.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if err := m.validateEntryKind(entry); err != nil {
		return err
	}

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	m.services.Delete(entry.ID)

	m.log.Debug("process service removed", zap.String("id", entry.ID.String()))
	return nil
}

var _ registry.EntryListener = (*Manager)(nil)
