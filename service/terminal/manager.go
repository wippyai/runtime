package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// Manager manages terminal instances registered via events
type Manager struct {
	ctx        context.Context
	log        *zap.Logger
	bus        events.Bus
	subscriber *eventbus.Subscriber
	terminals  map[string]*service
	loggerCore *LoggerInterceptor
}

// NewManager creates a new Manager instance
func NewManager(
	bus events.Bus,
	logger *zap.Logger,
	loggerCore *LoggerInterceptor,
) *Manager {
	return &Manager{
		log:        logger,
		bus:        bus,
		terminals:  make(map[string]*service),
		loggerCore: loggerCore,
	}
}

// Start initializes the manager and starts listening for events
func (m *Manager) Start(ctx context.Context) error {
	m.ctx = ctx

	// Subscribe to terminal events
	sub, err := eventbus.NewSubscriber(ctx, m.bus, terminal.System, "*", m.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to create event subscriber: %w", err)
	}
	m.subscriber = sub

	m.log.Info("terminal manager started")
	return nil
}

// Stop gracefully shuts down the manager
func (m *Manager) Stop() error {
	if m.subscriber != nil {
		m.subscriber.Close()
		m.subscriber = nil
	}

	return nil
}

func (m *Manager) handleEvent(e events.Event) {
	switch e.Kind {
	case terminal.RegisterTerminalEvent:
		app, ok := e.Data.(terminal.Application)
		if !ok {
			m.log.Error("invalid register terminal data", zap.String("id", string(e.Path)))
			return
		}
		m.handleRegister(string(e.Path), app)
	case terminal.DeleteTerminalEvent:
		m.handleDelete(string(e.Path))
	}
}

func (m *Manager) handleRegister(id string, app terminal.Application) {
	// todo: Check if alterady running and perform graceful migration
	// todo: or delegate it to underlying service

	// todo: can be an update!
	// todo: check if already running! (we will need runtime migration!)
	// create terminal application service
	term := newService(app.Terminal, app.Options, m.loggerCore)
	m.terminals[id] = term

	// register service if not already
	m.bus.Send(m.ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(id),
		Data: &supervisor.Entry{
			Service: term,
			Config:  app.Lifecycle,
		},
	})

	m.log.Info("term registered", zap.String("id", id))
}

func (m *Manager) handleDelete(id string) {
	// Check if terminal exists
	if _, exists := m.terminals[id]; !exists {
		m.log.Error("terminal not found for deletion", zap.String("id", id))
		return
	}

	// Notify supervisor
	m.bus.Send(m.ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   events.Path(id),
	})

	delete(m.terminals, id)
	m.log.Info("terminal deleted", zap.String("id", id))
}
