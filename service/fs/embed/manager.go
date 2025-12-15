package embed

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	entryutil "github.com/wippyai/runtime/internal/entry"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// Manager handles embedded filesystem registration and lifecycle.
type Manager struct {
	log         *zap.Logger
	bus         event.Bus
	dtt         payload.Transcoder
	embedReg    embedapi.Registry
	mu          sync.RWMutex
	filesystems sync.Map // map[string]fsapi.FS
}

// NewManager creates a new embed manager instance.
func NewManager(bus event.Bus, dtt payload.Transcoder, embedReg embedapi.Registry, logger *zap.Logger) *Manager {
	return &Manager{
		log:      logger,
		bus:      bus,
		dtt:      dtt,
		embedReg: embedReg,
	}
}

// Add creates and registers a new embedded filesystem.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return fsapi.NewUnsupportedEntryKindError(entry.Kind)
	}

	// Validate config can be decoded (embed doesn't use config content, filesystem comes from embedReg)
	if _, err := entryutil.DecodeEntryConfig[embedapi.Config](ctx, m.dtt, entry); err != nil {
		return fsapi.NewDecodeConfigError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicates
	if _, exists := m.filesystems.Load(entry.ID.String()); exists {
		return fsapi.NewFilesystemAlreadyExistsError(entry.ID.String())
	}

	if err := m.registerFS(ctx, entry.ID); err != nil {
		return err
	}

	m.log.Info("embedded filesystem registered", zap.String("id", entry.ID.String()))
	return nil
}

// Update updates an existing embedded filesystem.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return fsapi.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems.Load(entry.ID.String()); !exists {
		return fsapi.NewFilesystemNotFoundError(entry.ID.String())
	}

	// Remove old, register new
	m.removeFS(ctx, entry.ID)
	if err := m.registerFS(ctx, entry.ID); err != nil {
		return err
	}

	m.log.Info("embedded filesystem updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes an embedded filesystem.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return fsapi.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems.LoadAndDelete(entry.ID.String()); !exists {
		return fsapi.NewFilesystemNotFoundError(entry.ID.String())
	}

	m.removeFS(ctx, entry.ID)
	m.log.Info("embedded filesystem removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerFS retrieves the filesystem from embed registry and registers it.
func (m *Manager) registerFS(ctx context.Context, id registry.ID) error {
	// Get filesystem from embed registry
	packFS, err := m.embedReg.GetFS(id)
	if err != nil {
		m.log.Error("failed to get embedded filesystem",
			zap.String("id", id.String()),
			zap.Error(err))
		return systemfs.NewGetEmbeddedFilesystemError(err)
	}

	// Wrap in read-only adapter
	fs := fsapi.NewReadOnlyFS(packFS)

	// Store in filesystems map
	m.filesystems.Store(id.String(), fs)

	// Register with filesystem registry
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.KindRegister,
		Path:   id.String(),
		Data:   fs,
	})

	return nil
}

// removeFS removes the filesystem from the fs system.
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.KindDelete,
		Path:   id.String(),
	})
}
