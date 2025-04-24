package sql

import (
	"context"
	"fmt"
	ctxapi "github.com/ponyruntime/pony/api/context"
	"strconv"
	"sync"

	config "github.com/ponyruntime/pony/api/service/sql"
	config2 "github.com/ponyruntime/pony/internal/config"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/api/supervisor"
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
}

// NewManager creates a new SQL service manager
func NewManager(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
) (*Manager, error) {
	return NewManagerWithFactory(dtt, bus, log, NewDefaultPoolFactory())
}

// NewManagerWithFactory creates a new SQL service manager with the specified pool factory
func NewManagerWithFactory(
	dtt payload.Transcoder,
	bus event.Bus,
	log *zap.Logger,
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

	cfg, err := config2.DecodeAndInitConfig[config.DBConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	envCtx, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string])
	if !ok {
		return fmt.Errorf("cannot access env ctx")
	}

	if cfg.HostEnv != "" {
		cfg.Host, _ = envCtx.Value(cfg.HostEnv)
	}
	if cfg.PortEnv != "" {
		val, _ := envCtx.Value(cfg.PortEnv)
		cfg.Port, err = strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid port value: %w", err)
		}
	}
	if cfg.DatabaseEnv != "" {
		cfg.Database, _ = envCtx.Value(cfg.DatabaseEnv)
	}
	if cfg.UsernameEnv != "" {
		cfg.Username, _ = envCtx.Value(cfg.UsernameEnv)
	}
	if cfg.PasswordEnv != "" {
		cfg.Password, _ = envCtx.Value(cfg.PasswordEnv)
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

	cfg, err := config2.DecodeAndInitConfig[config.SQLiteConfig](m.dtt, entry)
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

	cfg, err := config2.DecodeAndInitConfig[config.DBConfig](m.dtt, entry)
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

	cfg, err := config2.DecodeAndInitConfig[config.SQLiteConfig](m.dtt, entry)
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
		zap.String("kind", string(entry.Kind)))
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
