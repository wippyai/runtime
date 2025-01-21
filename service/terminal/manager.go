package terminal

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/service/terminal"
	"sync"

	"github.com/ponyruntime/pony/api/events"
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
	mu         sync.RWMutex
	terminals  map[string]*service
}

// NewManager creates a new Manager instance
func NewManager(
	bus events.Bus,
	logger *zap.Logger,
	// todo: log interceptor!
) *Manager {
	return &Manager{
		log:       logger,
		bus:       bus,
		terminals: make(map[string]*service),
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
	case terminal.RegisterEvent:
		reg, ok := e.Data.(*terminal.Registration)
		if !ok {
			m.log.Error("invalid register terminal data", zap.String("event_path", string(e.Path)))
			return
		}
		m.handleRegister(string(e.Path), reg)
	case terminal.DeleteEvent:
		m.handleDelete(string(e.Path))
	}
}

func (m *Manager) handleRegister(id string, reg *terminal.Registration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// create terminal application service
	term := newService(reg.Terminal, reg.Config.Options)

	m.terminals[id] = term

	// register service
	m.bus.Send(m.ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   events.Path(id),
		Data: &supervisor.Entry{
			Service: term,
			Config:  reg.Config.Lifecycle,
		},
	})

	m.log.Info("term registered", zap.String("id", id))
}

func (m *Manager) handleDelete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

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
