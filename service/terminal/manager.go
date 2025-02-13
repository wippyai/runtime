package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/logs"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
)

// Manager handles shell service lifecycle and registration
type Manager struct {
	log   *zap.Logger
	bus   events.Bus
	dtt   payload.Transcoder
	mu    sync.RWMutex
	shell *Terminal
}

// NewShellManager creates a new shell manager instance
func NewShellManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{log: logger, bus: bus, dtt: dtt}
}

// Add creates and registers a new shell service
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(api.HostConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.shell = NewTerminal(entry.ID, cfg, logs.NewConfigSwitcher(m.bus, m.log), m.log)
	m.log.Info("terminal service created", zap.String("id", m.shell.id.String()))

	// Register as process host
	m.registerHost(ctx, m.shell)

	return nil
}

// Update updates an existing shell service
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg := new(api.HostConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shell == nil {
		return fmt.Errorf("terminal %s not found", entry.ID)
	}

	// Update service configuration
	err := m.shell.UpdateConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to update terminal config: %w", err)
	}

	m.log.Info("terminal service updated", zap.String("id", m.shell.id.String()))

	return nil
}

// Delete removes a shell service
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeHost(ctx, entry.ID)
	m.shell = nil // stop controlled by supervisor
	m.log.Info("terminal service removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerHost registers the shell service as a process host
func (m *Manager) registerHost(ctx context.Context, terminal *Terminal) {
	m.bus.Send(ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.RegisterHost,
		Path:   terminal.id.String(),
		Data:   process.Managed(terminal),
	})

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   terminal.id.String(),
		Data: &supervisor.Entry{
			Service: terminal,
			Config:  terminal.cfg.Lifecycle,
		},
	})
}

// removeHost removes the shell service from process host system
func (m *Manager) removeHost(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})

	m.bus.Send(ctx, events.Event{
		System: process.HostSystem,
		Kind:   process.DeleteHost,
		Path:   id.String(),
	})
}
