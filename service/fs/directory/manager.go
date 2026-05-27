// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"path"
	"path/filepath"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	moduleapi "github.com/wippyai/runtime/api/modules"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	entryutil "github.com/wippyai/runtime/internal/entry"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// Manager handles filesystem directory registration and lifecycle
type Manager struct {
	bus         event.Bus
	dtt         payload.Transcoder
	factory     FactoryAPI
	log         *zap.Logger
	directories map[registry.ID]fsapi.FS
	mu          sync.RWMutex
}

// NewDirectoryManager creates a new directory manager instance
func NewDirectoryManager(bus event.Bus, dtt payload.Transcoder, factory FactoryAPI, logger *zap.Logger) *Manager {
	if factory == nil {
		factory = NewFactory()
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:         logger,
		bus:         bus,
		dtt:         dtt,
		factory:     factory,
		directories: make(map[registry.ID]fsapi.FS),
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
	if _, exists := m.directories[entry.ID]; exists {
		return systemfs.NewFilesystemAlreadyExistsError(entry.ID.String())
	}

	return m.registerFSLocked(ctx, entry, cfg)
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

	old, exists := m.directories[entry.ID]
	if !exists {
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}

	if err := m.registerFSLocked(ctx, entry, cfg); err != nil {
		return err
	}

	if oldFS, ok := old.(interface{ Close() error }); ok && oldFS != nil {
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
	old, exists := m.directories[entry.ID]
	if !exists {
		m.mu.Unlock()
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}
	delete(m.directories, entry.ID)
	m.mu.Unlock()

	m.removeFS(ctx, entry.ID)

	if oldFS, ok := old.(interface{ Close() error }); ok && oldFS != nil {
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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registerFSLocked(ctx, registry.Entry{ID: id}, cfg)
}

func (m *Manager) registerFSLocked(ctx context.Context, entry registry.Entry, cfg *dirapi.Config) error {
	id := entry.ID
	dirPath := resolveDirectoryPath(ctx, entry, cfg)
	fs, err := m.factory.CreateFS(CreateFSConfig{
		DirPath:  dirPath,
		Mode:     cfg.GetMode(),
		AutoInit: cfg.AutoInit,
	})
	if err != nil {
		m.log.Error("failed to create filesystem instance",
			zap.String("id", id.String()),
			zap.String("directory", dirPath),
			zap.Error(err))
		return systemfs.NewCreateFilesystemError(err)
	}

	// Store in directories map
	m.directories[id] = fs

	// Register with filesystem registry
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsRegister,
		Path:   id.String(),
		Data:   fs,
	})

	m.log.Info("directory filesystem created",
		zap.String("id", id.String()),
		zap.String("path", dirPath))

	return nil
}

func resolveDirectoryPath(ctx context.Context, entry registry.Entry, cfg *dirapi.Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.Directory == "" || isAbsoluteConfiguredPath(cfg.Directory) {
		return cfg.Directory
	}
	if cfg.Base == dirapi.BaseProject {
		return cfg.Directory
	}

	moduleName := ""
	if entry.Meta != nil {
		moduleName = entry.Meta.GetString("module", "")
	}
	if moduleName == "" {
		return cfg.Directory
	}

	root, ok := moduleapi.SourceRoot(ctx, moduleName)
	if !ok {
		return cfg.Directory
	}

	return filepath.Join(root, cfg.Directory)
}

func isAbsoluteConfiguredPath(dir string) bool {
	return filepath.IsAbs(dir) || path.IsAbs(filepath.ToSlash(dir))
}

// removeFS removes the filesystem from the fs system
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.log.Debug("sending filesystem deletion event",
		zap.String("id", id.String()),
		zap.String("system", fsapi.System),
		zap.String("kind", fsapi.FsDelete),
		zap.String("path", id.String()))

	// Do regular registration
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsDelete,
		Path:   id.String(),
	})
}
