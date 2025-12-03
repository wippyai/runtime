package file

import (
	"context"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type Manager struct {
	log      *zap.Logger
	dtt      payload.Transcoder
	bus      event.Bus
	mu       sync.RWMutex
	storages map[registry.ID]*Storage
}

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

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindStorageFile {
		return env.NewUnsupportedKindError(entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[envsvc.FileStorageConfig](ctx, m.dtt, entry)
	if err != nil {
		return env.NewDecodeConfigError(err)
	}

	if err := cfg.Validate(); err != nil {
		return env.NewInvalidConfigError(err)
	}

	fileMode := os.FileMode(0644)
	if cfg.FileMode > 0 {
		fileMode = os.FileMode(cfg.FileMode)
	}

	dirMode := os.FileMode(0755)
	if cfg.DirMode > 0 {
		dirMode = os.FileMode(cfg.DirMode)
	}

	storage := NewStorage(cfg.FilePath, cfg.AutoCreate, fileMode, dirMode)

	m.mu.Lock()
	m.storages[entry.ID] = storage
	m.mu.Unlock()

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageRegister,
		Path:   entry.ID.String(),
		Data:   storage,
	})

	m.log.Info("registered file environment storage",
		zap.String("id", entry.ID.String()),
		zap.String("path", cfg.FilePath))

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	return m.Add(ctx, entry)
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindStorageFile {
		return env.NewUnsupportedKindError(entry.Kind)
	}

	m.mu.Lock()
	delete(m.storages, entry.ID)
	m.mu.Unlock()

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.StorageDelete,
		Path:   entry.ID.String(),
	})

	m.log.Info("deleted file environment storage",
		zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) GetStorage(id registry.ID) (env.Storage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	storage, exists := m.storages[id]
	if !exists {
		return nil, false
	}
	return storage, true
}
