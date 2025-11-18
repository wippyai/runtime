package embed

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	embedapi "github.com/wippyai/runtime/api/service/embed"
	entryutil "github.com/wippyai/runtime/internal/entry"
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
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	_, err := entryutil.DecodeEntryConfig[embedapi.Config](ctx, m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for duplicates
	if _, loaded := m.filesystems.LoadOrStore(entry.ID.String(), nil); loaded {
		return fmt.Errorf("embedded filesystem %s already exists", entry.ID)
	}

	return m.registerFS(ctx, entry.ID)
}

// Update updates an existing embedded filesystem.
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems.Load(entry.ID.String()); !exists {
		return fmt.Errorf("embedded filesystem %s not found", entry.ID)
	}

	// Remove old, register new
	m.removeFS(ctx, entry.ID)
	return m.registerFS(ctx, entry.ID)
}

// Delete removes an embedded filesystem.
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != embedapi.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.filesystems.LoadAndDelete(entry.ID.String()); !exists {
		return fmt.Errorf("embedded filesystem %s not found", entry.ID)
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
		return fmt.Errorf("failed to get embedded filesystem: %w", err)
	}

	// Wrap in read-only adapter
	fs := NewReadOnlyFS(packFS)

	// Store in filesystems map
	m.filesystems.Store(id.String(), fs)

	// Register with filesystem registry
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Register,
		Path:   id.String(),
		Data:   fs,
	})

	m.log.Info("embedded filesystem registered",
		zap.String("id", id.String()))

	return nil
}

// removeFS removes the filesystem from the fs system.
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	m.log.Debug("sending filesystem deletion event",
		zap.String("id", id.String()),
		zap.String("system", fsapi.System),
		zap.String("kind", fsapi.Delete),
		zap.String("path", id.String()))

	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Delete,
		Path:   id.String(),
	})
}
