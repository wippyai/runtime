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
	// VarState  = "var.state"
	// SetVar    = "var.set"
	// GetVar    = "var.get"
	// DeleteVar = "var.delete"
)

// Manager manages environment storage and handles environment-related events
type Manager struct {
	logger   *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]env.Storage
	factory  EnvStorageFactoryAPI
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
	m.logger.Info("received entry Add", zap.Any("id", entry))

	switch entry.Kind {
	case env.KindStorageMemory:
		return m.handleMemoryStorageAdd(ctx, entry)
	case env.KindStorageFile:
		return m.handleFileStorageAdd(ctx, entry)
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

	m.logger.Info("registered environment variable",
		zap.String("id", entry.ID.String()),
		zap.String("name", variable.Name),
		zap.String("env_name", variable.EnvName))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.logger.Info(fmt.Sprintf("received Update %s, %s", entry.ID, entry.Kind))

	switch entry.Kind {
	case env.KindStorageMemory, env.KindStorageFile:
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
	m.logger.Info(fmt.Sprintf("received Delete %s, %s", entry.ID, entry.Kind))

	switch entry.Kind {
	case env.KindStorageMemory, env.KindStorageFile:
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

	m.logger.Info("added env storage. entry",
		zap.Any("entry", entry),
	)

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

	// Register as registry storage
	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.logger.Info("added env storage",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}

//// Start starts the manager and subscribes to environment events
// func (m *Manager) Start(ctx context.Context) error {
//	m.ctx = ctx
//
//	// Subscribe to environment events
//	var err error
//	m.sub, err = eventbus.NewSubscriber(
//		ctx,
//		m.bus,
//		System,
//		"",
//		func(e event.Event) {
//			m.handleEvent(e)
//		},
//	)
//	if err != nil {
//		return fmt.Errorf("failed to subscribe to events: %w", err)
//	}
//
//	return nil
//}
//
//// Stop stops the manager and unsubscribes from events
// func (m *Manager) Stop() error {
//	if m.sub != nil {
//		m.sub.Close()
//	}
//	return nil
//}

// SetVar sets an environment variable
// func (m *Manager) SetVar(key, value string) {
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   SetVar,
//		Path:   key,
//		Data:   value,
//	})
//}

// GetVar gets an environment variable
// func (m *Manager) GetVar(key string) (string, bool) {
//	// Create a channel to receive the response
//	responseCh := make(chan event.Event, 1)
//
//	// Subscribe to VarState events for this key
//	sub, err := eventbus.NewSubscriber(
//		m.ctx,
//		m.bus,
//		System,
//		VarState,
//		func(e event.Event) {
//			if e.Path == key {
//				responseCh <- e
//			}
//		},
//	)
//	if err != nil {
//		return "", false
//	}
//	defer sub.Close()
//
//	// Send GetVar event
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   GetVar,
//		Path:   key,
//	})
//
//	// Wait for response
//	select {
//	case resp := <-responseCh:
//		if value, ok := resp.Data.(string); ok {
//			return value, true
//		}
//		return "", false
//	case <-time.After(time.Second):
//		return "", false
//	}
//}

// DeleteVar deletes an environment variable
// func (m *Manager) DeleteVar(key string) {
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   DeleteVar,
//		Path:   key,
//	})
//}

// handleEvent processes incoming environment events
// func (m *Manager) handleEvent(e event.Event) {
//	switch e.Kind {
//	case SetVar:
//		if value, ok := e.Data.(string); ok {
//			// Forward to all storages
//			for _, storage := range m.storages {
//				if err := storage.Set(m.ctx, e.Path, value); err != nil {
//					m.logger.Error("failed to set variable",
//						zap.String("key", e.Path),
//						zap.Error(err))
//				}
//			}
//			// Send state update
//			m.bus.Send(m.ctx, event.Event{
//				System: System,
//				Kind:   VarState,
//				Path:   e.Path,
//				Data:   value,
//			})
//		}
//	case GetVar:
//		// Try to get from first available storage
//		for _, storage := range m.storages {
//			if value, err := storage.Get(m.ctx, e.Path); err == nil {
//				m.bus.Send(m.ctx, event.Event{
//					System: System,
//					Kind:   VarState,
//					Path:   e.Path,
//					Data:   value,
//				})
//				return
//			}
//		}
//	case DeleteVar:
//		// Forward to all storages
//		for _, storage := range m.storages {
//			if err := storage.Delete(m.ctx, e.Path); err != nil {
//				m.logger.Error("failed to delete variable",
//					zap.String("key", e.Path),
//					zap.Error(err))
//			}
//		}
//		// Send state update
//		m.bus.Send(m.ctx, event.Event{
//			System: System,
//			Kind:   VarState,
//			Path:   e.Path,
//		})
//	}
//}
