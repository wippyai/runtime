// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"io/fs"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/fs/embed"
	entryutil "github.com/wippyai/runtime/system/entry"
	systemfs "github.com/wippyai/runtime/system/fs"
	"go.uber.org/zap"
)

// Manager handles embedded filesystem registration and lifecycle.
type Manager struct {
	bus         event.Bus
	dtt         payload.Transcoder
	embedReg    embedapi.Registry
	log         *zap.Logger
	filesystems map[registry.ID]fsapi.FS
	mu          sync.RWMutex
}

// NewManager creates a new embed manager instance.
func NewManager(bus event.Bus, dtt payload.Transcoder, embedReg embedapi.Registry, logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:         logger,
		bus:         bus,
		dtt:         dtt,
		embedReg:    embedReg,
		filesystems: make(map[registry.ID]fsapi.FS),
	}
}

// Add creates and registers a new embedded filesystem.
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	// Validate config can be decoded (embed doesn't use config content, filesystem comes from embedReg)
	if _, err := entryutil.DecodeEntryConfig[embedapi.Config](ctx, m.dtt, entry); err != nil {
		return systemfs.NewDecodeConfigError(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicates
	if _, exists := m.filesystems[entry.ID]; exists {
		return systemfs.NewFilesystemAlreadyExistsError(entry.ID.String())
	}

	if err := m.registerFS(ctx, entry); err != nil {
		return err
	}

	m.log.Info("embedded filesystem registered", zap.String("id", entry.ID.String()))
	return nil
}

// Update updates an existing embedded filesystem.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems[entry.ID]; !exists {
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}

	nextFS, err := m.fsForEntry(ctx, entry)
	if err != nil {
		return err
	}
	m.removeFS(ctx, entry.ID)
	m.storeFS(ctx, entry.ID, nextFS)

	m.log.Info("embedded filesystem updated", zap.String("id", entry.ID.String()))
	return nil
}

// Delete removes an embedded filesystem.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return systemfs.NewUnsupportedEntryKindError(entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems[entry.ID]; !exists {
		return systemfs.NewFilesystemNotFoundError(entry.ID.String())
	}
	delete(m.filesystems, entry.ID)

	m.removeFS(ctx, entry.ID)
	m.log.Info("embedded filesystem removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerFS retrieves the filesystem from the embed registry and registers it.
// Resolution is provenance-aware when the registry supports it, so updates
// select the pack of the module version the operation carries rather than an
// arbitrary pack that happens to expose the same resource ID.
func (m *Manager) registerFS(ctx context.Context, entry registry.Entry) error {
	fs, err := m.fsForEntry(ctx, entry)
	if err != nil {
		return err
	}
	m.storeFS(ctx, entry.ID, fs)
	return nil
}

func (m *Manager) fsForEntry(ctx context.Context, entry registry.Entry) (fsapi.FS, error) {
	packFS, err := m.resolveFS(ctx, entry)
	if err != nil {
		m.log.Error("failed to get embedded filesystem",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return nil, systemfs.NewGetEmbeddedFilesystemError(err)
	}
	return fsapi.NewReadOnlyFS(packFS), nil
}

func (m *Manager) storeFS(ctx context.Context, id registry.ID, fs fsapi.FS) {
	m.filesystems[id] = fs

	// Register with filesystem registry
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsRegister,
		Path:   id.String(),
		Data:   fs,
	})
}

// resolveFS prefers a provenance-aware lookup when the registry implements
// embedapi.EntryResolver, otherwise falls back to ID-based lookup. The
// provenance is the effective half of the operation the listener is handling;
// host entries and callers outside a transition supply none, which resolves by
// entry ID.
func (m *Manager) resolveFS(ctx context.Context, entry registry.Entry) (fs.ReadDirFS, error) {
	if resolver, ok := m.embedReg.(embedapi.EntryResolver); ok {
		return resolver.GetFSForEntry(entry, effectiveProvenance(ctx))
	}
	return m.embedReg.GetFS(entry.ID)
}

// effectiveProvenance returns the provenance of the entry the dispatched
// operation carries, or nil when the caller is not a provenance-carrying
// transition.
func effectiveProvenance(ctx context.Context) *registry.EntryProvenance {
	op, ok := registry.OpProvenanceFromContext(ctx)
	if !ok {
		return nil
	}
	return op.Effective
}

// removeFS removes the filesystem from the fs system.
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsDelete,
		Path:   id.String(),
	})
}
