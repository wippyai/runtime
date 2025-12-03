package os

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

// Manager handles OS storage lifecycle and registry integration.
type Manager struct {
	log       *zap.Logger
	dtt       payload.Transcoder
	bus       event.Bus
	mu        sync.RWMutex
	storages  map[registry.ID]env.Storage
	staticEnv map[string]string
}

// ManagerOption configures the Manager.
type ManagerOption func(*Manager)

// WithStaticEnv configures the manager to return StaticStorage instead of Storage
// when adding OS storage entries. This effectively replaces OS environment variable
// access with a predefined static set of key-value pairs.
func WithStaticEnv(staticEnv map[string]string) ManagerOption {
	return func(m *Manager) {
		m.staticEnv = staticEnv
	}
}

// NewManager creates a new OS storage manager.
func NewManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
	opts ...ManagerOption,
) *Manager {
	m := &Manager{
		log:      log,
		dtt:      dtt,
		bus:      bus,
		storages: make(map[registry.ID]env.Storage),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Add registers a new OS storage from registry entry.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindStorageOS {
		return env.NewUnsupportedKindError(entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[envsvc.OSStorageConfig](ctx, m.dtt, entry)
	if err != nil {
		return env.NewDecodeConfigError(err)
	}

	if err := cfg.Validate(); err != nil {
		return env.NewInvalidConfigError(err)
	}

	var storage env.Storage
	if m.staticEnv != nil {
		storage = NewStaticStorage(m.staticEnv)
	} else {
		storage = NewStorage()
	}

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.log.Info("registered OS environment storage",
		zap.String("id", entry.ID.String()))

	return nil
}

// Update updates an existing OS storage (recreates it).
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return m.Add(ctx, entry)
}

// Delete removes an OS storage.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindStorageOS {
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

	m.log.Info("deleted OS environment storage",
		zap.String("id", entry.ID.String()))

	return nil
}

// GetStorage retrieves a storage by ID.
func (m *Manager) GetStorage(id registry.ID) (env.Storage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storage, exists := m.storages[id]
	return storage, exists
}
