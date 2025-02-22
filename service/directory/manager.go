package directory

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	dirapi "github.com/ponyruntime/pony/api/service/directory"
	"go.uber.org/zap"
	"sync"
)

// Manager handles filesystem directory registration and lifecycle
type Manager struct {
	log         *zap.Logger
	bus         events.Bus
	dtt         payload.Transcoder
	mu          sync.RWMutex
	directories sync.Map // map[string]*DirectoryFS
}

// NewDirectoryManager creates a new directory manager instance
func NewDirectoryManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log: logger,
		bus: bus,
		dtt: dtt,
	}
}

// Add creates and registers a new directory filesystem
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(dirapi.Config)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store in directories map
	if _, loaded := m.directories.LoadOrStore(entry.ID.String(), nil); loaded {
		return fmt.Errorf("directory %s already exists", entry.ID)
	}

	m.log.Info("directory filesystem created",
		zap.String("id", entry.ID.String()),
		zap.String("path", cfg.Directory),
		zap.Bool("default", cfg.Default))

	// Register with fs system
	m.registerFS(ctx, entry.ID, cfg)

	return nil
}

// Update updates an existing directory filesystem
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(dirapi.Config)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.directories.Load(entry.ID.String()); !exists {
		return fmt.Errorf("directory %s not found", entry.ID)
	}

	m.log.Info("directory filesystem updated",
		zap.String("id", entry.ID.String()),
		zap.String("path", cfg.Directory))

	// Re-register with fs system
	m.registerFS(ctx, entry.ID, cfg)

	return nil
}

// Delete removes a directory filesystem
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != dirapi.Kind {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.directories.LoadAndDelete(entry.ID.String()); !exists {
		return fmt.Errorf("directory %s not found", entry.ID)
	}

	m.removeFS(ctx, entry.ID)
	m.log.Info("directory filesystem removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerFS registers the filesystem with the fs system
func (m *Manager) registerFS(ctx context.Context, id registry.ID, cfg *dirapi.Config) {
	// Register regular filesystem
	m.bus.Send(ctx, events.Event{
		System: fsapi.System,
		Kind:   fsapi.Register,
		Path:   id.String(),
		Data:   nil, // todo: Will be actual FS implementation
	})

	// Register default filesystem if configured
	if cfg.Default {
		m.bus.Send(ctx, events.Event{
			System: fsapi.System,
			Kind:   fsapi.RegisterDefault,
			Path:   id.String(),
			Data:   nil, // todo: Will be actual FS implementation
		})
	}
}

// removeFS removes the filesystem from the fs system
func (m *Manager) removeFS(ctx context.Context, id registry.ID) {
	// Remove from default if it was default
	m.bus.Send(ctx, events.Event{
		System: fsapi.System,
		Kind:   fsapi.DeleteDefault,
		Path:   id.String(),
	})

	// Remove regular registration
	m.bus.Send(ctx, events.Event{
		System: fsapi.System,
		Kind:   fsapi.Delete,
		Path:   id.String(),
	})
}
