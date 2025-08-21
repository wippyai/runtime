package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	serviceenv "github.com/ponyruntime/pony/api/service/env"
	"github.com/ponyruntime/pony/api/supervisor"
	internalconfig "github.com/ponyruntime/pony/internal/config"
	"go.uber.org/zap"
)

const (
	System = env.System
)

// Manager manages environment storage and handles environment-related events
type Manager struct {
	logger   *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]env.Storage
	factory  StorageFactoryAPI
}

// NewManager creates a new environment storage manager instance
func NewManager(bus event.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		bus:      bus,
		dtt:      dtt,
		logger:   logger,
		storages: make(map[registry.ID]env.Storage),
		factory:  NewDefaultEnvStorageFactory(),
	}
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case env.KindStorageMemory:
		return m.handleMemoryStorageAdd(ctx, entry)
	case env.KindStorageFile:
		return m.handleFileStorageAdd(ctx, entry)
	case env.KindStorageOS:
		return m.handleOSStorageAdd(ctx, entry)
	case env.KindStorageRouter:
		return m.handleRouterStorageAdd(ctx, entry)
	case env.KindVariable:
		return m.handleVariableAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) handleMemoryStorageAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.CreateMemoryEnvStorageConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	storage, err := m.factory.CreateMemoryEnvStorage(entry.Kind, cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create env storage: %w", err)
	}

	m.storages[entry.ID] = storage

	return m.registerService(ctx, entry, storage, cfg.Lifecycle)
}

func (m *Manager) handleFileStorageAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.CreateFileEnvStorageConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	storage, err := m.factory.CreateFileEnvStorage(entry.Kind, cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create env storage: %w", err)
	}

	m.storages[entry.ID] = storage

	return m.registerService(ctx, entry, storage, cfg.Lifecycle)
}

func (m *Manager) handleOSStorageAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.CreateOSEnvStorageConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	storage, err := m.factory.CreateOSEnvStorage(entry.Kind, cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create env storage: %w", err)
	}

	m.storages[entry.ID] = storage

	return m.registerService(ctx, entry, storage, cfg.Lifecycle)
}

func (m *Manager) handleRouterStorageAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.CreateRouterEnvStorageConfig](m.dtt, entry)
	if err != nil {
		m.logger.Error("failed to decode router config", zap.Error(err))
		return err
	}

	// Create router storage
	routerStorage, err := m.factory.CreateRouterEnvStorage(entry.Kind, cfg, m.storages, m.logger)
	if err != nil {
		m.logger.Error("failed to create router env storage", zap.Error(err))
		return fmt.Errorf("failed to create router env storage: %w", err)
	}

	// Resolve the referenced storages
	var storages []env.Storage
	for _, storageName := range cfg.Storages {
		storageID := registry.ParseID(storageName)
		m.logger.Debug("resolving storage reference",
			zap.String("storage_name", storageName),
			zap.String("storage_id", storageID.String()))

		if storage, exists := m.storages[storageID]; exists {
			m.logger.Debug("storage found",
				zap.String("storage_name", storageName),
				zap.String("storage_id", storageID.String()))
			storages = append(storages, storage)
		} else {
			m.logger.Debug("referenced storage not found",
				zap.String("storage_name", storageName),
				zap.String("storage_id", storageID.String()),
				zap.String("router_id", entry.ID.String()))
		}
	}

	// Create a new router storage with the resolved storages
	if len(storages) > 0 {
		routerStorage, err = NewRouterStorage(storages, m.logger)
		if err != nil {
			m.logger.Error("failed to create router storage with resolved storages", zap.Error(err))
			return fmt.Errorf("failed to create router storage with resolved storages: %w", err)
		}
	} else {
		m.logger.Warn("no storages resolved, using placeholder router storage",
			zap.String("id", entry.ID.String()))
	}

	m.storages[entry.ID] = routerStorage

	return m.registerService(ctx, entry, routerStorage, cfg.Lifecycle)
}

func (m *Manager) handleVariableAdd(ctx context.Context, entry registry.Entry) error {
	var variable env.Variable
	if err := m.dtt.Unmarshal(entry.Data, &variable); err != nil {
		return fmt.Errorf("failed to decode variable: %w", err)
	}

	// Send variable registration event
	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   entry.ID.String(),
		Data:   variable,
	})

	m.logger.Debug("registered environment variable",
		zap.String("id", entry.ID.String()),
		zap.String("name", variable.Name),
		zap.String("env_name", variable.EnvName))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case env.KindStorageMemory, env.KindStorageFile, env.KindStorageOS, env.KindStorageRouter:
		storage, ok := entry.Data.(env.Storage)
		if !ok {
			return fmt.Errorf("invalid storage type")
		}

		m.mu.Lock()
		m.storages[entry.ID] = storage
		m.mu.Unlock()

		m.logger.Debug("environment storage updated",
			zap.String("path", entry.ID.String()),
			zap.Any("value", storage),
		)
	case env.KindVariable:
		var variable env.Variable
		if err := m.dtt.Unmarshal(entry.Data, &variable); err != nil {
			return fmt.Errorf("failed to decode variable: %w", err)
		}

		// Send variable update event
		m.bus.Send(ctx, event.Event{
			System: env.System,
			Kind:   env.VariableUpdate,
			Path:   entry.ID.String(),
			Data:   variable,
		})

		m.logger.Debug("environment variable updated",
			zap.String("path", entry.ID.String()),
			zap.String("name", variable.Name),
			zap.String("env_name", variable.EnvName))
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case env.KindStorageMemory, env.KindStorageFile, env.KindStorageOS, env.KindStorageRouter:
		m.mu.Lock()
		delete(m.storages, entry.ID)
		m.mu.Unlock()

		m.logger.Debug("environment storage deleted",
			zap.String("path", entry.ID.String()))
	case env.KindVariable:
		// Send variable delete event
		m.bus.Send(ctx, event.Event{
			System: env.System,
			Kind:   env.VariableDelete,
			Path:   entry.ID.String(),
		})

		m.logger.Debug("environment variable deleted",
			zap.String("path", entry.ID.String()))
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	return nil
}

// Acquire implements resource.Provider interface
func (m *Manager) Acquire(_ context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.storages[id]
	if !exists {
		return nil, fmt.Errorf("storage %s not found", id)
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, resource.ErrResourceLocked
	}

	return &memoryResource{
		storage: m.storages[id].(*MemoryStorage),
		id:      id,
		closed:  false,
		mu:      sync.Mutex{},
	}, nil
}

// registerService handles the common service registration logic
func (m *Manager) registerService(ctx context.Context, entry registry.Entry, storage env.Storage, lifecycle supervisor.LifecycleConfig) error {
	m.storages[entry.ID] = storage

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: storage,
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
			Provider: storage,
			Meta:     map[string]interface{}{"type": entry.Kind},
		},
	})

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.logger.Debug("added env storage",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}
