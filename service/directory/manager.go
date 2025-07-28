package directory

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	dirapi "github.com/ponyruntime/pony/api/service/directory"
	"go.uber.org/zap"
)

// Manager handles filesystem directory registration and lifecycle
type Manager struct {
	log         *zap.Logger
	bus         event.Bus
	dtt         payload.Transcoder
	factory     FSFactoryAPI
	mu          sync.RWMutex
	directories sync.Map // map[string]*FS
}

// NewDirectoryManager creates a new directory manager instance
func NewDirectoryManager(bus event.Bus, dtt payload.Transcoder, factory FSFactoryAPI, logger *zap.Logger) *Manager {
	if factory == nil {
		factory = NewDirectoryFSFactory()
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
		zap.String("path", cfg.Directory))

	return m.registerFS(ctx, entry.ID, cfg)
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

	return m.registerFS(ctx, entry.ID, cfg)
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

func (m *Manager) registerFS(ctx context.Context, id registry.ID, cfg *dirapi.Config) error {
	fs, err := m.factory.CreateFS(CreateFSConfig{
		Name:     cfg.Type,
		DirPath:  cfg.Directory,
		Mode:     cfg.FileMode(),
		AutoInit: cfg.AutoInit,
	})
	if err != nil {
		m.log.Error("failed to create filesystem instance",
			zap.String("id", id.String()),
			zap.String("directory", cfg.Directory),
			zap.Error(err))
		return fmt.Errorf("failed to create filesystem: %w", err)
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
		zap.String("system", string(fsapi.System)),
		zap.String("kind", string(fsapi.Delete)),
		zap.String("path", id.String()))

	// Done regular registration
	m.bus.Send(ctx, event.Event{
		System: fsapi.System,
		Kind:   fsapi.Delete,
		Path:   id.String(),
	})
}
