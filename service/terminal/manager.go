package terminal

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/system/eventbus"
	"go.uber.org/zap"
)

type Manager struct {
	ctx      context.Context
	log      *zap.Logger
	bus      events.Bus
	dtt      payload.Transcoder
	sub      *eventbus.Subscriber
	mu       sync.RWMutex
	services map[string]*service
	apps     map[string]*api.Terminal
}

func NewManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log:      logger,
		bus:      bus,
		dtt:      dtt,
		services: make(map[string]*service),
		apps:     make(map[string]*api.Terminal),
	}
}

// Start initializes the manager and starts listening for events
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx

	// Subscribe to terminal events
	sub, err := eventbus.NewSubscriber(ctx, m.bus, api.System, "*", m.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to create event subscriber: %w", err)
	}
	m.sub = sub

	m.log.Info("terminal manager started")
	return nil
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() error {
	if m.sub != nil {
		m.sub.Close()
		m.sub = nil
	}

	return nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindTerminal {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	cfg, err := m.fetchConfig(entry)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	app, exists := m.apps[cfg.Process.String()]
	if !exists {
		return fmt.Errorf("terminal app %s not found", cfg.Process)
	}

	svc := newService(*app, entry.ID, cfg, m.bus, m.log)
	m.services[entry.ID.String()] = svc

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(entry.ID.String()),
		Data: &supervisor.Entry{
			Service: svc,
			Config:  cfg.Lifecycle,
		},
	})

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cfg, err := m.fetchConfig(entry)
	if err != nil {
		return err
	}

	svc, exists := m.services[entry.ID.String()]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	app, exists := m.apps[cfg.Process.String()]
	if !exists {
		return fmt.Errorf("terminal app %s not found", cfg.Process)
	}

	if err := svc.UpdateApp(ctx, *app, cfg.Process.String()); err != nil {
		return fmt.Errorf("failed to update service: %w", err)
	}

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   events.Path(entry.ID),
		Data:   &supervisor.Entry{Config: cfg.Lifecycle},
	})

	return nil
}

func (m *Manager) fetchConfig(entry registry.Entry) (*api.ServiceConfig, error) {
	cfg := new(api.ServiceConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cfg: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cfg: %w", err)
	}

	cfg.InitDefaults()
	return cfg, nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	delete(m.services, entry.ID.String())
	m.mu.Unlock()

	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   events.Path(entry.ID),
	})

	return nil
}

func (m *Manager) handleEvent(e events.Event) {
	switch e.Kind {
	case api.RegisterTerminalEvent:
		app, ok := e.Data.(api.Terminal)
		if !ok {
			m.log.Error("invalid register terminal data", zap.String("id", string(e.Path)))
			return
		}

		m.mu.Lock()
		m.apps[registry.Name(e.Path)] = &app

		// Update any running services using this app
		found := false
		for _, svc := range m.services {
			if svc.terminal.id == registry.Name(e.Path) {
				err := svc.UpdateApp(m.ctx, app, registry.Name(e.Path))
				if err != nil {
					m.log.Error("failed to update service",
						zap.String("id", string(e.Path)),
						zap.Error(err))
				}

				found = true
				m.log.Info("updated terminal application", zap.String("id", string(e.Path)))
			}
		}
		m.mu.Unlock()

		if !found {
			m.log.Info("registered terminal application", zap.String("id", string(e.Path)))
		}
	case api.DeleteTerminalEvent:
		m.mu.Lock()
		delete(m.apps, registry.Name(e.Path))
		m.mu.Unlock()

		m.log.Info("deleted terminal application", zap.String("id", string(e.Path)))
	}
}
