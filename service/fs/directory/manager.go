package directory

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	entryutil "github.com/wippyai/runtime/internal/entry"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// Manager handles filesystem directory registration and lifecycle
type Manager struct {
	log         *zap.Logger
	bus         event.Bus
	dtt         payload.Transcoder
	factory     FactoryAPI
	mu          sync.RWMutex
	directories sync.Map // map[string]*FS
}

// NewDirectoryManager creates a new directory manager instance
func NewDirectoryManager(bus event.Bus, dtt payload.Transcoder, factory FactoryAPI, logger *zap.Logger) *Manager {
	if factory == nil {
		factory = NewFactory()
	}
	return &Manager{
		log:     logger,
		bus:     bus,
		dtt:     dtt,
		factory: factory,
	}
}

// Add creates and registers a new directory filesystem
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[dirapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return systemfs.NewDecodeConfigError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store in directories map
	if _, loaded := m.directories.LoadOrStore(entry.ID.String(), nil); loaded {
		return systemfs.NewFilesystemAlreadyExistsError(entry.ID.String())
	}

	return m.registerFS(ctx, entry.ID, cfg)
}

// Update updates an existing directory filesystem
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[dirapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return systemfs.NewDecodeConfigError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	old, exists := m.directories.Load(entry.ID.String())
	if !exists {
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}

	if err := m.registerFS(ctx, entry.ID, cfg); err != nil {
		return err
	}

	if oldFS, ok := old.(*FS); ok && oldFS != nil {
		if err := oldFS.Close(); err != nil {
			m.log.Warn("failed to close old filesystem during update",
				zap.String("id", entry.ID.String()),
				zap.Error(err))
		}
	}

	m.log.Info("directory filesystem updated",
		zap.String("id", entry.ID.String()),
		zap.String("path", cfg.Directory))

	return nil
}

// Delete removes a directory filesystem
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	old, exists := m.directories.LoadAndDelete(entry.ID.String())
	if !exists {
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}

	m.removeFS(ctx, entry.ID)

	if oldFS, ok := old.(*FS); ok && oldFS != nil {
		if err := oldFS.Close(); err != nil {
			m.log.Warn("failed to close old filesystem",
				zap.String("id", entry.ID.String()),
				zap.Error(err))
		}
	}

	m.log.Info("directory filesystem removed", zap.String("id", entry.ID.String()))

	return nil
}

func (m *Manager) registerFS(ctx context.Context, id registry.ID, cfg *dirapi.Config) error {
	fs, err := m.factory.CreateFS(CreateFSConfig{
		DirPath:  cfg.Directory,
		Mode:     cfg.GetMode(),
		AutoInit: cfg.AutoInit,
	})
	if err != nil {
		m.log.Error("failed to create filesystem instance",
			zap.String("id", id.String()),
			zap.String("directory", cfg.Directory),
			zap.Error(err))
		return systemfs.NewCreateFilesystemError(err)
	}

	// Store in directories map
	m.directories.Store(id.String(), fs)

	// Register with filesystem registry
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Register,
		Path:   id.String(),
		Data:   fs,
	})

	m.log.Info("directory filesystem created",
		zap.String("id", id.String()),
		zap.String("path", cfg.Directory))

	return nil
}

// removeFS removes the filesystem from the fs system
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.log.Debug("sending filesystem deletion event",
		zap.String("id", id.String()),
		zap.String("system", fsapi.System),
		zap.String("kind", fsapi.Delete),
		zap.String("path", id.String()))

	// Do regular registration
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Delete,
		Path:   id.String(),
	})
}
