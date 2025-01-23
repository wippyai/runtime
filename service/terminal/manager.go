package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
	"sync"
)

type Manager struct {
	ctx      context.Context
	log      *zap.Logger
	bus      events.Bus
	dtt      payload.Transcoder
	sub      *eventbus.Subscriber
	mu       sync.RWMutex
	services map[registry.ID]*service
	apps     map[registry.ID]*api.Application
}

func NewManager(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Manager {
	return &Manager{
		log:      logger,
		bus:      bus,
		dtt:      dtt,
		services: make(map[registry.ID]*service),
		apps:     make(map[registry.ID]*api.Application),
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

	cfg := new(api.ServiceConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	app, exists := m.apps[cfg.Target]
	if !exists {
		return fmt.Errorf("terminal app %s not found", cfg.Target)
	}

	svc := newService(app.Terminal, app.Options, cfg.Target, cfg.Timeouts, m.bus)
	m.services[entry.ID] = svc

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(entry.ID),
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

	cfg := new(api.ServiceConfig)
	if err := m.dtt.Unmarshal(entry.Data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	svc, exists := m.services[entry.ID]
	if !exists {
		return fmt.Errorf("service %s not found", entry.ID)
	}

	app, exists := m.apps[cfg.Target]
	if !exists {
		return fmt.Errorf("terminal app %s not found", cfg.Target)
	}

	if err := svc.UpdateApp(ctx, app.Terminal, app.Options, cfg.Target); err != nil {
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

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	delete(m.services, entry.ID)
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
		app, ok := e.Data.(api.Application)
		if !ok {
			m.log.Error("invalid register terminal data", zap.String("id", string(e.Path)))
			return
		}

		m.mu.Lock()
		m.apps[registry.ID(e.Path)] = &app

		// Update any running services using this app
		for _, svc := range m.services {
			if instance, ok := svc.lifecycle.Current(); ok && instance.id == registry.ID(e.Path) {
				err := svc.UpdateApp(m.ctx, app.Terminal, app.Options, registry.ID(e.Path))
				if err != nil {
					m.log.Error("failed to update service",
						zap.String("id", string(e.Path)),
						zap.Error(err))
				}
			}
		}
		m.mu.Unlock()

	case api.DeleteTerminalEvent:
		m.mu.Lock()
		delete(m.apps, registry.ID(e.Path))
		m.mu.Unlock()
	}
}
