package memory

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// Manager handles memory storage lifecycle and registry integration.
type Manager struct {
	log      *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]*Storage
}

// NewManager creates a new memory storage manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *Manager {
	return &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		storages: make(map[registry.ID]*Storage),
	}
}

// Add registers a new memory storage from registry entry.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.StorageMemory {
		return env.NewUnsupportedKindError(entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[envsvc.MemoryStorageConfig](ctx, m.dtt, entry)
	if err != nil {
		return env.NewDecodeConfigError(err)
	}

	if err := cfg.Validate(); err != nil {
		return env.NewInvalidConfigError(err)
	}

	storage := NewStorage(nil)

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	// Register directly in the central registry for synchronous access
	if reg := env.GetRegistry(ctx); reg != nil {
		reg.RegisterStorage(entry.ID, storage)
	}

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.log.Info("registered memory environment storage",
		zap.String("id", entry.ID.String()))

	return nil
}

// Update updates an existing memory storage (recreates it).
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return m.Add(ctx, entry)
}

// Delete removes a memory storage.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.StorageMemory {
		return env.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	_, exists := m.storages[entry.ID]
	if !exists {
		m.mu.Unlock()
		return env.NewStorageNotExistsError(entry.ID.String())
	}
	delete(m.storages, entry.ID)
	m.mu.Unlock()

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("deleted memory environment storage",
		zap.String("id", entry.ID.String()))

	return nil
}

// GetStorage retrieves a storage by ID.
func (m *Manager) GetStorage(id registry.ID) (env.Storage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storage, exists := m.storages[id]
	if !exists {
		return nil, false
	}
	return storage, true
}
