// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	dirapi "github.com/wippyai/runtime/api/service/fs/directory"
	entryutil "github.com/wippyai/runtime/system/entry"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// fsReplyKinds matches the filesystem registry replies to a request.
const fsReplyKinds = "fs.(accept|reject)"

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
		DirPath:    dirPath,
		Mode:       cfg.GetMode(),
		AutoInit:   cfg.AutoInit,
		ReadOnly:   cfg.ReadOnly,
		LinkPolicy: cfg.LinkPolicy,
	})
	if err != nil {
		m.log.Error("failed to create filesystem instance",
			zap.String("id", id.String()),
			zap.String("directory", dirPath),
			zap.Error(err))
		return systemfs.NewCreateFilesystemError(err)
	}

	if err := m.sendFS(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsRegister,
		Path:   id.String(),
		Data:   fsapi.Request{FS: fs},
	}); err != nil {
		if closer, ok := fs.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		return err
	}
	m.directories[id] = fs

	m.log.Info("directory filesystem created",
		zap.String("id", id.String()),
		zap.String("path", dirPath))

	return nil
}

func resolveDirectoryPath(ctx context.Context, entry registry.Entry, cfg *dirapi.Config) string {
	return dirapi.ResolveDirectory(ctx, entry, cfg)
}

// removeFS removes the filesystem from the fs system
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.log.Debug("sending filesystem deletion event",
		zap.String("id", id.String()),
		zap.String("system", fsapi.System),
		zap.String("kind", fsapi.FsDelete),
		zap.String("path", id.String()))

	if err := m.sendFS(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsDelete,
		Path:   id.String(),
		Data:   fsapi.Request{},
	}); err != nil {
		m.log.Warn("filesystem deletion was not confirmed",
			zap.String("id", id.String()),
			zap.Error(err))
	}
}

// sendFS sends a filesystem registry request and, when the runtime provides
// request coordination, returns once the registry accepts it. The registry
// stores handles on its own subscriber: without the confirmation, the next
// listener in the same registry transition, such as an http.static entry,
// can look up the filesystem before it is stored, and the transition fails
// with "filesystem not found" once its retry pass makes no progress.
func (m *Manager) sendFS(ctx context.Context, request event.Event) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil || !m.bus.HasSubscribers(fsapi.System, request.Kind) {
		m.bus.Send(ctx, request)
		return nil
	}
	opID := "fs.op/" + uuid.NewString()
	data := request.Data.(fsapi.Request)
	data.OpID = opID
	request.Data = data
	waiter, err := awaitSvc.Prepare(ctx, fsapi.System, fsReplyKinds, opID, event.ContextBoundAwait)
	if err != nil {
		return systemfs.NewFilesystemRegistrationError(request.Path, request.Kind, err)
	}
	defer waiter.Close()

	m.bus.Send(ctx, request)

	result := waiter.Wait()
	if result.Error != nil {
		return systemfs.NewFilesystemRegistrationError(request.Path, request.Kind, result.Error)
	}
	if !result.Accepted {
		return systemfs.NewFilesystemRegistrationError(request.Path, request.Kind, fmt.Errorf("rejected: %v", result.Event.Data))
	}
	return nil
}
