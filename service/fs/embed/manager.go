// SPDX-License-Identifier: MPL-2.0

package embed

import (
	"context"
	"fmt"
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

// fsReplyKinds matches the filesystem registry replies to a request.
const fsReplyKinds = "fs.(accept|reject)"

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

	nextFS, err := m.fsForEntry(entry)
	if err != nil {
		return err
	}
	if err := m.storeFS(ctx, entry.ID, nextFS); err != nil {
		return err
	}

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
	if err := m.removeFS(ctx, entry.ID); err != nil {
		return err
	}
	delete(m.filesystems, entry.ID)
	m.log.Info("embedded filesystem removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerFS retrieves the active filesystem resource and registers it.
func (m *Manager) registerFS(ctx context.Context, entry registry.Entry) error {
	fs, err := m.fsForEntry(entry)
	if err != nil {
		return err
	}
	return m.storeFS(ctx, entry.ID, fs)
}

func (m *Manager) fsForEntry(entry registry.Entry) (fsapi.FS, error) {
	packFS, err := m.embedReg.GetFS(entry.ID)
	if err != nil {
		m.log.Error("failed to get embedded filesystem",
			zap.String("id", entry.ID.String()),
			zap.Error(err))
		return nil, systemfs.NewGetEmbeddedFilesystemError(err)
	}
	return fsapi.NewReadOnlyFS(packFS), nil
}

// storeFS publishes fs to the filesystem registry and returns once the
// registry serves it. The registry stores handles on its own subscriber, so
// without the confirmation the next registry listener in the same transition
// can still resolve the previous handle. An Update replaces the served handle
// in one step, leaving no window in which the filesystem is absent.
func (m *Manager) storeFS(ctx context.Context, id registry.ID, fs fsapi.FS) error {
	if err := m.awaitFS(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsRegister,
		Path:   id.String(),
		Data:   fs,
	}); err != nil {
		return err
	}
	m.filesystems[id] = fs
	return nil
}

// removeFS withdraws the filesystem from the filesystem registry and returns
// once the registry no longer serves it.
func (m *Manager) removeFS(ctx context.Context, id registry.ID) error {
	return m.awaitFS(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.FsDelete,
		Path:   id.String(),
	})
}

// awaitFS sends a filesystem registry request and waits for its accept or
// reject. Replies are correlated by filesystem path; the Manager serializes
// its operations under mu and awaits each reply, so no earlier request for
// the same path is outstanding when a waiter is prepared.
func (m *Manager) awaitFS(ctx context.Context, request event.Event) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return systemfs.NewFilesystemRegistrationError(request.Path, request.Kind, systemfs.ErrRegistrationCoordinationUnavailable)
	}
	waiter, err := awaitSvc.Prepare(ctx, fsapi.System, fsReplyKinds, request.Path, 0)
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
