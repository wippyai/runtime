package supervisor

import (
	"context"
	"fmt"
	processapi "github.com/ponyruntime/pony/api/service/supervisor"
	"github.com/ponyruntime/pony/internal/config"
	"github.com/ponyruntime/pony/system/process"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// ServiceFactory is an interface for creating service instances
type ServiceFactory interface {
	// CreateService creates a new service instance with the given configuration
	CreateService(id registry.ID, config processapi.ServiceConfig) supervisor.Service
}

// Manager handles process services lifecycle and monitoring
type Manager struct {
	log      *zap.Logger
	bus      event.Bus
	proc     *process.Manager
	services sync.Map // map[registry.ID]supervisor.Service
	factory  ServiceFactory
}

// NewSupervisorServiceManager creates a new process service manager
func NewSupervisorServiceManager(
	bus event.Bus,
	proc *process.Manager,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:     log,
		bus:     bus,
		proc:    proc,
		factory: NewDefaultServiceFactory(),
	}
}

// NewSupervisorServiceManagerWithFactory creates a new process service manager with factory
func NewSupervisorServiceManagerWithFactory(
	bus event.Bus,
	proc *process.Manager,
	log *zap.Logger,
	factory ServiceFactory,
) *Manager {
	return &Manager{
		log:     log,
		bus:     bus,
		proc:    proc,
		factory: factory,
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Unmarshal config
	cfg, err := config.DecodeAndInitConfig[processapi.ServiceConfig](payload.GetTranscoder(ctx), entry)
	if err != nil {
		return err
	}

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	// Create service instance
	var svc supervisor.Service
	if m.factory != nil {
		svc = m.factory.CreateService(entry.ID, *cfg)
	} else {
		svc = &Service{
			id:     entry.ID,
			config: *cfg,
			status: make(chan any, 1),
		}
	}

	// Store service
	m.services.Store(entry.ID, svc)

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
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
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Get existing service
	_, exists := m.services.Load(entry.ID)
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	// Unmarshal new config
	cfg, err := config.DecodeAndInitConfig[processapi.ServiceConfig](payload.GetTranscoder(ctx), entry)
	if err != nil {
		return err
	}

	cfg.Process = cfg.Process.WithDefaultNS(entry.ID.NS)

	// Update supervisor config
	m.bus.Send(ctx, event.Event{
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
	if entry.Kind != processapi.KindProcessService {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, processapi.KindProcessService)
	}

	// Done from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Done from services map
	m.services.Delete(entry.ID)

	m.log.Info("process supervisor removed", zap.String("id", entry.ID.String()))

	return nil
}
