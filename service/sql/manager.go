// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"errors"
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
	dtt      payload.Transcoder
	bus      event.Bus
	factory  PoolFactoryAPI
	env      envapi.Registry
	log      *zap.Logger
	services map[registry.ID]*ConnPool
	mu       sync.RWMutex
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
		return nil, ErrTranscoderRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if factory == nil {
		return nil, ErrPoolFactoryRequired
	}
	if log == nil {
		log = zap.NewNop()
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
	case config.Postgres, config.MySQL:
		return m.handleStandardDBAdd(ctx, entry)
	case config.SQLite:
		return m.handleSQLiteAdd(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.Postgres, config.MySQL:
		return m.handleStandardDBUpdate(ctx, entry)
	case config.SQLite:
		return m.handleSQLiteUpdate(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
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
		return NewServiceExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}

	if err := m.resolveDBConfigEnv(ctx, cfg); err != nil {
		return err
	}

	pool, err := m.factory.CreateStandardPool(ctx, entry.Kind, cfg)
	if err != nil {
		return NewConnectionPoolCreationError(err)
	}

	return m.registerService(ctx, entry, pool, cfg.Lifecycle)
}

func (m *Manager) handleSQLiteAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.services[entry.ID]; exists {
		return NewServiceExistsError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}

	pool, err := m.factory.CreateSQLitePool(ctx, cfg)
	if err != nil {
		return NewSQLiteConnectionCreationError(err)
	}

	return m.registerService(ctx, entry, pool, cfg.Lifecycle)
}

func (m *Manager) handleStandardDBUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.DBConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}

	if err := m.resolveDBConfigEnv(ctx, cfg); err != nil {
		return err
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return NewPoolUpdateError(err)
	}

	m.updateService(ctx, entry, cfg.Lifecycle)
	return nil
}

func (m *Manager) handleSQLiteUpdate(ctx context.Context, entry registry.Entry) error {
	pool, exists := m.services[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
	}

	cfg, err := entryutil.DecodeEntryConfig[config.SQLiteConfig](ctx, m.dtt, entry)
	if err != nil {
		return NewInvalidConfigError(err)
	}

	if err := pool.UpdateConfig(cfg); err != nil {
		return NewSQLiteUpdateError(err)
	}

	m.updateService(ctx, entry, cfg.Lifecycle)
	return nil
}

func (m *Manager) handleDBDelete(ctx context.Context, entry registry.Entry) error {
	_, exists := m.services[entry.ID]
	if !exists {
		return NewServiceNotFoundError(entry.ID)
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
		Kind:   supervisor.ServiceRegister,
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
			Meta:     map[string]any{"type": entry.Kind},
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
		Kind:   supervisor.ServiceUpdate,
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
		Kind:   supervisor.ServiceRemove,
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

func (m *Manager) resolveDBConfigEnv(ctx context.Context, cfg *config.DBConfig) error {
	var err error
	if v, rerr := m.resolveEnv(ctx, cfg.HostEnv, "host"); rerr != nil {
		return rerr
	} else if v != "" {
		cfg.Host = v
	}
	if v, rerr := m.resolveEnv(ctx, cfg.PortEnv, "port"); rerr != nil {
		return rerr
	} else if v != "" {
		cfg.Port, err = strconv.Atoi(v)
		if err != nil {
			return NewInvalidPortError(cfg.PortEnv, err)
		}
	}
	if v, rerr := m.resolveEnv(ctx, cfg.DatabaseEnv, "database"); rerr != nil {
		return rerr
	} else if v != "" {
		cfg.Database = v
	}
	if v, rerr := m.resolveEnv(ctx, cfg.UsernameEnv, "username"); rerr != nil {
		return rerr
	} else if v != "" {
		cfg.Username = v
	}
	if v, rerr := m.resolveEnv(ctx, cfg.PasswordEnv, "password"); rerr != nil {
		return rerr
	} else if v != "" {
		cfg.Password = v
	}
	if len(cfg.OptionsEnv) > 0 {
		resolved := make(map[string]string, len(cfg.OptionsEnv))
		for optKey, envVar := range cfg.OptionsEnv {
			v, rerr := m.resolveEnv(ctx, envVar, "options."+optKey)
			if rerr != nil {
				return rerr
			}
			if v != "" {
				resolved[optKey] = v
			}
		}
		if len(resolved) > 0 {
			if cfg.Options == nil {
				cfg.Options = make(map[string]string)
			}
			for k, v := range resolved {
				cfg.Options[k] = v
			}
		}
	}
	return nil
}

func (m *Manager) resolveEnv(ctx context.Context, envVar, field string) (string, error) {
	if envVar == "" {
		return "", nil
	}
	if m.env == nil {
		return "", NewUnresolvedEnvError(field, envVar, errors.New("env registry not configured"))
	}
	val, found, err := m.env.Lookup(ctx, envVar)
	if err != nil {
		return "", NewUnresolvedEnvError(field, envVar, err)
	}
	if !found {
		return "", NewUnresolvedEnvError(field, envVar, envapi.ErrVariableNotFound)
	}
	return val, nil
}
