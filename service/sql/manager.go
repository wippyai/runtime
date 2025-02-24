package sql

import (
	"context"
	"fmt"
	config "github.com/ponyruntime/pony/api/service/sql"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// Manager handles SQL database connections lifecycle and resource provisioning
type Manager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus events.Bus

	mu       sync.RWMutex
	services map[registry.ID]*ConnPool
}

// NewManager creates a new SQL service manager
func NewManager(
	dtt payload.Transcoder,
	bus events.Bus,
	log *zap.Logger,
) (*Manager, error) {
	if dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}

	return &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		services: make(map[registry.ID]*ConnPool),
	}, nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.KindPostgres, config.KindMySQL:
		return m.handleStandardDBAdd(ctx, entry)
	case config.KindSQLite:
		return m.handleSQLiteAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.KindPostgres, config.KindMySQL:
		return m.handleStandardDBUpdate(ctx, entry)
	case config.KindSQLite:
		return m.handleSQLiteUpdate(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.handleDBDelete(ctx, entry)
}

func (m *Manager) handleStandardDBAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.services[entry.ID]; exists {
		return fmt.Errorf("service %s already exists", entry.ID)
	}

	cfg, err := decodeAndInitConfig[config.DBConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	pool, err := NewStandardConnPool(entry.Kind, cfg)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	return m.registerService(ctx, entry, pool, cfg.Lifecycle)
}

func (m *Manager) handleSQLiteAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.services[entry.ID]; exists {
		return fmt.Errorf("service %s already exists", entry.ID)
	}

	cfg, err := decodeAndInitConfig[config.SQLiteConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	cfg.FS = cfg.FS.WithDefaultNS(entry.ID.NS)
	pool, err := NewSQLiteConnPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create SQLite connection: %w", err)
	}

	return m.registerService(ctx, entry, pool, cfg.Lifecycle)
}

func (m *Manager) handleStandardDBUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	cfg, err := decodeAndInitConfig[config.DBConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("failed to update pool config: %w", err)
	}

	m.updateService(ctx, entry, cfg.Lifecycle)
	return nil
}

func (m *Manager) handleSQLiteUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	cfg, err := decodeAndInitConfig[config.SQLiteConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("failed to update SQLite config: %w", err)
	}

	m.updateService(ctx, entry, cfg.Lifecycle)
	return nil
}

func (m *Manager) handleDBDelete(ctx context.Context, entry registry.Entry) error {
	_, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	m.unregisterService(ctx, entry)
	delete(m.services, entry.ID)
	return nil
}

// registerService handles the common service registration logic
func (m *Manager) registerService(ctx context.Context, entry registry.Entry, pool *ConnPool, lifecycle supervisor.LifecycleConfig) error {
	m.services[entry.ID] = pool

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: pool,
			Config:  lifecycle,
		},
	})

	// Register as resource provider
	m.bus.Send(ctx, events.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: pool,
			Meta:     map[string]interface{}{"type": string(entry.Kind)},
		},
	})

	m.log.Info("added database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	return nil
}

// updateService handles the common service update logic
func (m *Manager) updateService(ctx context.Context, entry registry.Entry, lifecycle supervisor.LifecycleConfig) {
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: lifecycle,
		},
	})

	m.log.Info("updated database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))
}

// unregisterService handles the common service unregistration logic
func (m *Manager) unregisterService(ctx context.Context, entry registry.Entry) {
	// Delete from supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Delete resource provider
	m.bus.Send(ctx, events.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("removed database service",
		zap.String("id", entry.ID.String()))
}

// decodeAndInitConfig decodes the configuration and initializes defaults
func decodeAndInitConfig[T any](dtt payload.Transcoder, entry registry.Entry) (*T, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("configuration data is required")
	}

	cfg := new(T)
	if err := dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Initialize defaults if the config implements InitDefaults
	if defaulter, ok := interface{}(cfg).(interface{ InitDefaults() }); ok {
		defaulter.InitDefaults()
	}

	// Validate if the config implements Validate
	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return cfg, nil
}
