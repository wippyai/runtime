package terminal

import (
	"context"
	"fmt"
	supervisor2 "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/logs"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"go.uber.org/zap"
)

// Manager handles terminal service lifecycle and registration
type Manager struct {
	log      *zap.Logger
	bus      events.Bus
	dtt      payload.Transcoder
	mu       sync.RWMutex
	terminal *Terminal
}

// NewTerminalManager creates a new terminal manager instance
func NewTerminalManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{log: logger, bus: bus, dtt: dtt}
}

// Add creates and registers a new terminal service
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

	m.terminal = NewTerminal(entry.ID, cfg, logs.NewConfigSwitcher(m.bus, m.log), m.log)
	m.log.Info("terminal service created", zap.String("id", m.terminal.id.String()))

	// Register as process host
	m.registerHost(ctx, m.terminal)

	return nil
}

// Update updates an existing terminal service
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

	if m.terminal == nil {
		return fmt.Errorf("terminal %s not found", entry.ID)
	}

	// Update service configuration
	err := m.terminal.UpdateConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to update terminal config: %w", err)
	}

	m.log.Info("terminal service updated", zap.String("id", m.terminal.id.String()))

	return nil
}

// Delete removes a terminal service
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeHost(ctx, entry.ID)
	m.terminal = nil // stop controlled by supervisor
	m.log.Info("terminal service removed", zap.String("id", entry.ID.String()))

	return nil
}

// registerHost registers the terminal service as a process host
func (m *Manager) registerHost(ctx context.Context, terminal *Terminal) {
	// connect to pubsub
	m.bus.Send(ctx, events.Event{
		System: pubsub.System,
		Kind:   pubsub.RegisterHost,
		Path:   terminal.id.String(),
		Data:   pubsub.Host(terminal),
	})

	// we can host processes
	m.bus.Send(ctx, events.Event{
		System: supervisor2.HostSystem,
		Kind:   supervisor2.RegisterHost,
		Path:   terminal.id.String(),
		Data:   supervisor2.Managed(terminal),
	})

	// we run!
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   terminal.id.String(),
		Data: &supervisor.Entry{
			Service: terminal,
			Config:  terminal.config.Lifecycle,
		},
	})
}

// removeHost removes the terminal service from process host system
func (m *Manager) removeHost(ctx context.Context, id registry.ID) {
	// disconnect from pubsub
	m.bus.Send(ctx, events.Event{
		System: pubsub.System,
		Kind:   pubsub.DeleteHost,
		Path:   id.String(),
	})

	// we can no longer host processes
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})

	// we no longer run!
	m.bus.Send(ctx, events.Event{
		System: supervisor2.HostSystem,
		Kind:   supervisor2.DeleteHost,
		Path:   id.String(),
	})
}
