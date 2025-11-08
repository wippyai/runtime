package env

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/env"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	envsvc "github.com/ponyruntime/pony/api/service/env"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

type Manager struct {
	logger   *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]env.Storage
	factory  StorageFactory
}

func NewManager(bus event.Bus, dtt payload.Transcoder, logger *zap.Logger, factory StorageFactory) *Manager {
	return &Manager{
		bus:      bus,
		dtt:      dtt,
		logger:   logger,
		storages: make(map[registry.ID]env.Storage),
		factory:  factory,
	}
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case envsvc.KindStorageMemory:
		return m.handleMemoryStorageAdd(ctx, entry)
	case envsvc.KindStorageFile:
		return m.handleFileStorageAdd(ctx, entry)
	case envsvc.KindStorageOS:
		return m.handleOSStorageAdd(ctx, entry)
	case envsvc.KindStorageRouter:
		return m.handleRouterStorageAdd(ctx, entry)
	case envsvc.KindVariable:
		return m.handleVariableAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) handleMemoryStorageAdd(ctx context.Context, entry registry.Entry) error {
	var cfg envsvc.MemoryStorageConfig
	if err := m.dtt.Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to decode memory storage config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid memory storage config: %w", err)
	}

	storage, err := m.factory.CreateMemoryEnvStorage(&cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create memory storage: %w", err)
	}

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	return m.registerService(ctx, entry, storage)
}

func (m *Manager) handleFileStorageAdd(ctx context.Context, entry registry.Entry) error {
	var cfg envsvc.FileStorageConfig
	if err := m.dtt.Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to decode file storage config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid file storage config: %w", err)
	}

	storage, err := m.factory.CreateFileEnvStorage(&cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create file storage: %w", err)
	}

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	return m.registerService(ctx, entry, storage)
}

func (m *Manager) handleOSStorageAdd(ctx context.Context, entry registry.Entry) error {
	var cfg envsvc.OSStorageConfig
	if err := m.dtt.Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to decode OS storage config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid OS storage config: %w", err)
	}

	storage, err := m.factory.CreateOSEnvStorage(&cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create OS storage: %w", err)
	}

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	return m.registerService(ctx, entry, storage)
}

func (m *Manager) handleRouterStorageAdd(ctx context.Context, entry registry.Entry) error {
	var cfg envsvc.RouterStorageConfig
	if err := m.dtt.Unmarshal(entry.Data, &cfg); err != nil {
		return fmt.Errorf("failed to decode router storage config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid router storage config: %w", err)
	}

	m.mu.RLock()
	storagesCopy := make(map[registry.ID]env.Storage)
	for k, v := range m.storages {
		storagesCopy[k] = v
	}
	m.mu.RUnlock()

	storage, err := m.factory.CreateRouterEnvStorage(&cfg, storagesCopy, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create router storage: %w", err)
	}

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	return m.registerService(ctx, entry, storage)
}

func (m *Manager) handleVariableAdd(ctx context.Context, entry registry.Entry) error {
	var variable env.Variable
	if err := m.dtt.Unmarshal(entry.Data, &variable); err != nil {
		return fmt.Errorf("failed to decode variable: %w", err)
	}
	variable.ID = entry.ID

	if err := variable.Validate(); err != nil {
		return fmt.Errorf("invalid variable: %w", err)
	}

	m.mu.RLock()
	_, storageExists := m.storages[variable.StorageID]
	m.mu.RUnlock()

	if !storageExists {
		return fmt.Errorf("referenced storage not found: %s", variable.StorageID.String())
	}

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   entry.ID.String(),
		Data:   variable,
	})

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case envsvc.KindStorageMemory, envsvc.KindStorageFile, envsvc.KindStorageOS, envsvc.KindStorageRouter:
		return m.Add(ctx, entry)
	case envsvc.KindVariable:
		var variable env.Variable
		if err := m.dtt.Unmarshal(entry.Data, &variable); err != nil {
			return fmt.Errorf("failed to decode variable: %w", err)
		}
		variable.ID = entry.ID

		if err := variable.Validate(); err != nil {
			return fmt.Errorf("invalid variable: %w", err)
		}

		m.mu.RLock()
		_, storageExists := m.storages[variable.StorageID]
		m.mu.RUnlock()

		if !storageExists {
			return fmt.Errorf("referenced storage not found: %s", variable.StorageID.String())
		}

		m.bus.Send(ctx, event.Event{
			System: env.System,
			Kind:   env.VariableUpdate,
			Path:   entry.ID.String(),
			Data:   variable,
		})

		m.logger.Info("updated environment variable",
			zap.String("id", entry.ID.String()),
			zap.String("name", variable.Name))
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case envsvc.KindStorageMemory, envsvc.KindStorageFile, envsvc.KindStorageOS, envsvc.KindStorageRouter:
		m.mu.Lock()
		delete(m.storages, entry.ID)
		m.mu.Unlock()

		m.bus.Send(ctx, event.Event{
			System: env.System,
			Kind:   env.StorageDelete,
			Path:   entry.ID.String(),
		})

		m.logger.Info("deleted environment storage",
			zap.String("id", entry.ID.String()))
	case envsvc.KindVariable:
		m.bus.Send(ctx, event.Event{
			System: env.System,
			Kind:   env.VariableDelete,
			Path:   entry.ID.String(),
		})

		m.logger.Info("deleted environment variable",
			zap.String("id", entry.ID.String()))
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	return nil
}

func (m *Manager) GetStorage(id registry.ID) (env.Storage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storage, exists := m.storages[id]
	return storage, exists
}

func (m *Manager) ListStorages() map[registry.ID]env.Storage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[registry.ID]env.Storage)
	for k, v := range m.storages {
		result[k] = v
	}
	return result
}

func (m *Manager) registerService(ctx context.Context, entry registry.Entry, storage env.Storage) error {
	svc, ok := storage.(supervisor.Service)
	if !ok {
		return nil
	}

	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Service: svc,
		},
	})

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.logger.Info("registered environment storage",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}
