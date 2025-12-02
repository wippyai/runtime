package sql

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	envapi "github.com/wippyai/runtime/api/env"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	config "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles SQL database connections lifecycle and resource provisioning
type Manager struct {
	log     *zap.Logger
	dtt     payload.Transcoder
	bus     event.Bus
	factory PoolFactoryAPI

	mu       sync.RWMutex
	services map[registry.ID]*ConnPool
	env      envapi.Registry
}

// NewManager creates a new SQL service manager
func NewManager(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
	envRegistry envapi.Registry,
) (*Manager, error) {
	return NewManagerWithFactory(dtt, bus, log, envRegistry, NewDefaultPoolFactory())
}

// NewManagerWithFactory creates a new SQL service manager with the specified pool factory
func NewManagerWithFactory(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
	envRegistry envapi.Registry,
	factory PoolFactoryAPI,
) (*Manager, error) {
	if dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if factory == nil {
		return nil, fmt.Errorf("pool factory is required")
	}

	return &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		factory:  factory,
		services: make(map[registry.ID]*ConnPool),
		env:      envRegistry,
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

	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	if cfg.HostEnv != "" {
		val, found, err := m.env.Lookup(ctx, cfg.HostEnv)
		if err != nil {
			m.log.Warn("failed to lookup host env var", zap.String("var", cfg.HostEnv), zap.Error(err))
		} else if found {
			cfg.Host = val
		} else {
			m.log.Warn("host env var not found", zap.String("var", cfg.HostEnv))
		}
	}
	if cfg.PortEnv != "" {
		val, found, err := m.env.Lookup(ctx, cfg.PortEnv)
		if err != nil {
			m.log.Warn("failed to lookup port env var", zap.String("var", cfg.PortEnv), zap.Error(err))
		} else if found {
			cfg.Port, err = strconv.Atoi(val)
			if err != nil {
				return fmt.Errorf("invalid port value from env %s: %w", cfg.PortEnv, err)
			}
		} else {
			m.log.Warn("port env var not found", zap.String("var", cfg.PortEnv))
		}
	}
	if cfg.DatabaseEnv != "" {
		val, found, err := m.env.Lookup(ctx, cfg.DatabaseEnv)
		if err != nil {
			m.log.Warn("failed to lookup database env var", zap.String("var", cfg.DatabaseEnv), zap.Error(err))
		} else if found {
			cfg.Database = val
		} else {
			m.log.Warn("database env var not found", zap.String("var", cfg.DatabaseEnv))
		}
	}
	if cfg.UsernameEnv != "" {
		val, found, err := m.env.Lookup(ctx, cfg.UsernameEnv)
		if err != nil {
			m.log.Warn("failed to lookup username env var", zap.String("var", cfg.UsernameEnv), zap.Error(err))
		} else if found {
			cfg.Username = val
		} else {
			m.log.Warn("username env var not found", zap.String("var", cfg.UsernameEnv))
		}
	}
	if cfg.PasswordEnv != "" {
		val, found, err := m.env.Lookup(ctx, cfg.PasswordEnv)
		if err != nil {
			m.log.Warn("failed to lookup password env var", zap.String("var", cfg.PasswordEnv), zap.Error(err))
		} else if found {
			cfg.Password = val
		} else {
			m.log.Warn("password env var not found", zap.String("var", cfg.PasswordEnv))
		}
	}

	pool, err := m.factory.CreateStandardPool(entry.Kind, cfg)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	return m.registerService(ctx, entry, pool, cfg.Lifecycle)
}

func (m *Manager) handleSQLiteAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.services[entry.ID]; exists {
		return fmt.Errorf("service %s already exists", entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	pool, err := m.factory.CreateSQLitePool(cfg)
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

	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, m.dtt, entry)
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

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
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
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: pool,
			Config:  lifecycle,
		},
	})

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   entry.ID.String(),
		Data: resource.Entry{
			ID:       entry.ID,
			Provider: pool,
			Meta:     map[string]interface{}{"type": entry.Kind},
		},
	})

	m.log.Info("added database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}

// updateService handles the common service update logic
func (m *Manager) updateService(ctx context.Context, entry registry.Entry, lifecycle supervisor.LifecycleConfig) {
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: lifecycle,
		},
	})

	m.log.Info("updated database service",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))
}

// unregisterService handles the common service unregistration logic
func (m *Manager) unregisterService(ctx context.Context, entry registry.Entry) {
	// Delete from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Delete resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   entry.ID.String(),
		Data:   entry.ID,
	})

	m.log.Info("removed database service",
		zap.String("id", entry.ID.String()))
}
