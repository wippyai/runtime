package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/system/process"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	processApi "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// Manager handles process services lifecycle and monitoring
type Manager struct {
	log      *zap.Logger
	bus      events.Bus
	proc     *process.Manager
	services sync.Map // map[registry.ID]*ProcessService
}

// NewProcessSupervisorManager creates a new process service manager
func NewProcessSupervisorManager(
	bus events.Bus,
	proc *process.Manager,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:  log,
		bus:  bus,
		proc: proc,
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processApi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processApi.KindProcessService)
	}

	// Unmarshal config
	var cfg processApi.ServiceConfig
	if err := payload.GetTranscoder(ctx).Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create service instance
	svc := &Service{
		id:     entry.ID,
		config: cfg,
		status: make(chan any, 1),
	}

	// Store service
	m.services.Store(entry.ID, svc)

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: svc,
			Config:  cfg.Lifecycle,
		},
	})

	m.log.Info("process supervisor added", zap.String("id", entry.ID.String()))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processApi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processApi.KindProcessService)
	}

	// Get existing service
	_, exists := m.services.Load(entry.ID)
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	// Unmarshal new config
	var cfg processApi.ServiceConfig
	if err := payload.GetTranscoder(ctx).Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// todo: at the moment we do not update config and do not swap ids and etc

	// Update supervisor config
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("process supervisor updated", zap.String("id", entry.ID.String()))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processApi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processApi.KindProcessService)
	}

	// Remove from supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Remove from services map
	m.services.Delete(entry.ID)

	m.log.Info("process supervisor removed", zap.String("id", entry.ID.String()))

	return nil
}
