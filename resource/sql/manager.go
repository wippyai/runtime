package sql

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	config "github.com/ponyruntime/pony/api/resource/sql"
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

	cfg, err := decodeEntity[config.DBConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	pool, err := NewStandardConnPool(entry.Kind, cfg)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	m.services[entry.ID] = pool

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: pool,
			Config:  cfg.Lifecycle,
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

func (m *Manager) handleSQLiteAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.services[entry.ID]; exists {
		return fmt.Errorf("service %s already exists", entry.ID)
	}

	cfg, err := decodeEntity[config.SQLiteConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	pool, err := NewSQLiteConnPool(cfg)
	if err != nil {
		return fmt.Errorf("failed to create SQLite connection: %w", err)
	}

	m.services[entry.ID] = pool

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: pool,
			Config:  cfg.Lifecycle,
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

	m.log.Info("added SQLite service",
		zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) handleStandardDBUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	cfg, err := decodeEntity[config.DBConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("failed to update pool config: %w", err)
	}

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("updated database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	return nil
}

func (m *Manager) handleSQLiteUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	cfg, err := decodeEntity[config.SQLiteConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("failed to update SQLite config: %w", err)
	}

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("updated SQLite service",
		zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) handleDBDelete(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	// Remove from supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Remove resource provider
	m.bus.Send(ctx, events.Event{
		System: resource.System,
		Kind:   resource.Remove,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	delete(m.services, entry.ID)

	if err := pool.Close(); err != nil {
		m.log.Error("failed to close connection pool",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
	}

	return nil
}

// Helper to decode entities
func decodeEntity[T any](entry registry.Entry, transcoder payload.Transcoder) (*T, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("configuration data is required")
	}

	cfg := new(T)
	if err := transcoder.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return cfg, nil
}
