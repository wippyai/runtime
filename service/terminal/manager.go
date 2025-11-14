package terminal

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/supervisor"
	entryutil "github.com/wippyai/runtime/internal/entry"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/terminal"
	"go.uber.org/zap"
)

// Manager handles terminal service lifecycle and registration
type Manager struct {
	log      *zap.Logger
	bus      event.Bus
	dtt      payload.Transcoder
	mu       sync.RWMutex
	terminal *Terminal
	factory  ServiceFactory
}

// NewTerminalManager creates a new terminal manager instance
func NewTerminalManager(bus event.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log:     logger,
		bus:     bus,
		dtt:     dtt,
		factory: NewDefaultServiceFactory(bus, logger),
	}
}

// NewTerminalManagerWithFactory creates a new terminal manager with a custom factory
func NewTerminalManagerWithFactory(
	bus event.Bus,
	dtt payload.Transcoder,
	logger *zap.Logger,
	factory ServiceFactory,
) *Manager {
	return &Manager{
		log:     logger,
		bus:     bus,
		dtt:     dtt,
		factory: factory,
	}
}

// Add creates and registers a new terminal service
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindHost {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg, err := entryutil.DecodeEntryConfig[api.HostConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Factory now handles log switcher creation internally
	m.terminal = m.factory.CreateTerminal(entry.ID, cfg)
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

	cfg, err := entryutil.DecodeEntryConfig[api.HostConfig](ctx, m.dtt, entry)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.terminal == nil {
		return fmt.Errorf("terminal %s not found", entry.ID)
	}

	// Update service configuration
	err = m.terminal.UpdateConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to update terminal config: %w", err)
	}

	m.registerHost(ctx, m.terminal)

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
	// connect to relay
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   terminal.id.String(),
		Data:   relay.Host(terminal),
	})

	// we can host processes
	m.bus.Send(ctx, event.Event{
		System: process.HostSystem,
		Kind:   process.HostRegister,
		Path:   terminal.id.String(),
		Data:   process.Managed(terminal),
	})

	// we run!
	m.bus.Send(ctx, event.Event{
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
	// disconnect from relay
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   id.String(),
	})

	// we can no longer host processes
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})

	// we no longer run!
	m.bus.Send(ctx, event.Event{
		System: process.HostSystem,
		Kind:   process.HostDelete,
		Path:   id.String(),
	})
}
