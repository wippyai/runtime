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
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

// System is the event system identifier for environment events
//const System = "env"

// Event kinds
const (
	SetStorage    = "set_storage"    // Set environment storage
	GetStorage    = "get_storage"    // Get environment storage
	DeleteStorage = "delete_storage" // Delete environment storage
	StorageState  = "storage_state"  // Response with storage state
)

// Manager manages environment storage and handles environment-related events
type Manager struct {
	ctx      context.Context
	logger   *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	sub      *eventbus.Subscriber
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

//
//// Start initializes the manager and begins listening for environment events
//func (m *Manager) Start(ctx context.Context) error {
//	m.ctx = ctx
//
//	// Subscribe to environment storage events
//	sub, err := eventbus.NewSubscriber(
//		ctx,
//		m.bus,
//		System,
//		"(set_storage|get_storage|delete_storage)",
//		m.handleEvent,
//	)
//	if err != nil {
//		return fmt.Errorf("failed to create subscriber: %w", err)
//	}
//	m.sub = sub
//
//	m.logger.Info("environment storage manager started")
//	return nil
//}

//// Stop gracefully shuts down the manager
//func (m *Manager) Stop() error {
//	if m.sub != nil {
//		m.sub.Close()
//		m.sub = nil
//	}
//	m.logger.Info("environment storage manager stopped")
//	return nil
//}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.logger.Info(fmt.Sprintf("received Add %s, %s", entry.ID, entry.Kind))

	switch entry.Kind {
	case env.KindMemory:
		return m.handleMemoryStorageAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	//
	//if entry.Kind != env.KindMemory && entry.Kind != env.KindFile {
	//	return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	//}
	//
	//storage, ok := entry.Data.(env.Storage)
	//if !ok {
	//	return fmt.Errorf("invalid storage type")
	//}
	//
	//m.mu.Lock()
	//m.storages[entry.ID] = storage
	//m.mu.Unlock()
	//
	//m.logger.Debug("environment storage added",
	//	zap.String("path", entry.ID.String()),
	//	zap.Any("value", storage),
	//)
	//
	//meta, err := m.set(ctx, entry)
	//if err != nil {
	//	return fmt.Errorf("add entry: %w", err)
	//}
	//
	//m.bus.Send(ctx, event.Event{
	//	System: env.System,
	//	Kind:   env.StorageRegister,
	//	Path:   entry.ID.String(),
	//	Data: resource.Entry{
	//		ID:       entry.ID,
	//		Meta:     meta,
	//		Provider: m, // Manager itself is the provider
	//	},
	//})
	//
	//return nil
}

func (m *Manager) handleMemoryStorageAdd(ctx context.Context, entry registry.Entry) error {
	if _, exists := m.storages[entry.ID]; exists {
		return fmt.Errorf("service %s already exists", entry.ID)
	}

	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.StorageMemoryConfig](m.dtt, entry)
	if err != nil {
		return err
	}

	//envCtx, ok := ctx.Value(ctxapi.EnvCtx).(*ctxapi.Contexter[string])
	//if !ok {
	//	return fmt.Errorf("cannot access env ctx")
	//}

	storage, err := m.factory.CreateMemoryEnvStorage(entry.Kind, cfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create env storage: %w", err)
	}

	return m.registerService(ctx, entry, storage, cfg.Lifecycle)
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.logger.Info(fmt.Sprintf("received Update %s, %s", entry.ID, entry.Kind))

	if entry.Kind != env.KindMemory && entry.Kind != env.KindFile {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

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

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.logger.Info(fmt.Sprintf("received Delete %s, %s", entry.ID, entry.Kind))

	if entry.Kind != env.KindMemory && entry.Kind != env.KindFile {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	delete(m.storages, entry.ID)
	m.mu.Unlock()

	m.logger.Debug("environment storage deleted",
		zap.String("path", entry.ID.String()),
	)

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

	m.logger.Info("added env storage",
		zap.String("id", entry.ID.String()),
		zap.String("kind", entry.Kind))

	return nil
}

//
//func (m *Manager) set(ctx context.Context, entry registry.Entry) (registry.Metadata, error) {
//	// Decode and initialize configuration
//	cfg, err := internalconfig.DecodeAndInitConfig[serviceenv.StorageMemoryConfig](m.dtt, entry)
//	if err != nil {
//		return nil, fmt.Errorf("decode config: %w", err)
//	}
//
//	resourceRegistry := resource.GetResources(ctx)
//	rsc, err := resourceRegistry.Acquire(ctx, registry.ParseID(cfg.Name), resource.ModeNormal)
//	if err != nil {
//		return nil, fmt.Errorf("acquire resource: %w", err)
//	}
//
//	gotConfig, err := rsc.Get()
//	if err != nil {
//		return nil, fmt.Errorf("get config: %w", err)
//	}
//
//	storageCfg, ok := gotConfig.(serviceenv.StorageMemoryConfig)
//	if !ok {
//		return nil, fmt.Errorf("aws config not config")
//	}
//
//	//// Create S3 client
//	//client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
//	//	if cfg.Endpoint != "" {
//	//		o.UsePathStyle = true
//	//		o.BaseEndpoint = aws.String(cfg.Endpoint)
//	//	}
//	//})
//
//	// Create S3 storage
//	storage := NewMemoryStorage(nil, m.logger)
//
//	m.storages[entry.ID] = storage
//	return map[string]any{
//		"name": cfg.Name,
//	}, nil
//}

//// s3Resource represents an acquired S3 storage resource
//type s3Resource struct {
//	manager *Manager
//	id      registry.ID
//	closed  bool
//	mu      sync.Mutex
//}
//
//// Get implements resource.Resource interface
//func (r *s3Resource) Get() (any, error) {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if r.closed {
//		return nil, resource.ErrResourceReleased
//	}
//
//	// Ensure storage still exists in manager
//	r.manager.mu.RLock()
//	storage, exists := r.manager.storages[r.id]
//	r.manager.mu.RUnlock()
//
//	if !exists {
//		return nil, resource.ErrResourceReleased
//	}
//
//	return cloudstorage.Storage(storage), nil
//}
//
//// Release implements resource.Resource interface
//func (r *s3Resource) Release() {
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	if r.closed {
//		return
//	}
//
//	r.closed = true
//}

//
//// handleEvent processes incoming environment events
//func (m *Manager) handleEvent(e event.Event) {
//	switch e.Kind {
//	case SetStorage:
//		m.handleSetStorage(e)
//	case GetStorage:
//		m.handleGetStorage(e)
//	case DeleteStorage:
//		m.handleDeleteStorage(e)
//	default:
//		m.logger.Warn("unknown event kind",
//			zap.String("kind", e.Kind),
//			zap.String("path", e.Path),
//		)
//	}
//}
//
//// handleSetStorage processes environment storage set events
//func (m *Manager) handleSetStorage(e event.Event) {
//	m.mu.Lock()
//	m.storages[e.Path] = e.Data.(env.Storage)
//	m.mu.Unlock()
//
//	m.logger.Debug("environment storage set",
//		zap.String("path", e.Path),
//		zap.Any("value", e.Data),
//	)
//
//	// Send confirmation
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   StorageState,
//		Path:   e.Path,
//		Data:   e.Data,
//	})
//}
//
//// handleGetStorage processes environment storage get events
//func (m *Manager) handleGetStorage(e event.Event) {
//	m.mu.RLock()
//	value, exists := m.storages[e.Path]
//	m.mu.RUnlock()
//
//	if !exists {
//		m.logger.Debug("environment storage not found",
//			zap.String("path", e.Path),
//		)
//		return
//	}
//
//	// Send response with storage value
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   StorageState,
//		Path:   e.Path,
//		Data:   value,
//	})
//}
//
//// handleDeleteStorage processes environment storage delete events
//func (m *Manager) handleDeleteStorage(e event.Event) {
//	m.mu.Lock()
//	delete(m.storages, e.Path)
//	m.mu.Unlock()
//
//	m.logger.Debug("environment storage deleted",
//		zap.String("path", e.Path),
//	)
//
//	// Send confirmation
//	m.bus.Send(m.ctx, event.Event{
//		System: System,
//		Kind:   StorageState,
//		Path:   e.Path,
//	})
//}

//// GetStorage returns the value of an environment storage
//func (m *Manager) GetStorage(path string) (env.Storage, bool) {
//	m.mu.RLock()
//	defer m.mu.RUnlock()
//	value, exists := m.storages[path]
//	return value, exists
//}

//// SetStorage sets the value of an environment storage
//func (m *Manager) SetStorage(path string, value env.Storage) {
//	m.mu.Lock()
//	m.storages[path] = value
//	m.mu.Unlock()
//}
//
//// DeleteStorage removes an environment storage
//func (m *Manager) DeleteStorage(path string) {
//	m.mu.Lock()
//	delete(m.storages, path)
//	m.mu.Unlock()
//}
