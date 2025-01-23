package logs

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Manager manages logging configuration and event handling
type Manager struct {
	log       *zap.Logger
	bus       events.Bus
	core      api.Core
	mu        sync.RWMutex
	config    api.Config
	eventChan chan events.Event
	subID     events.SubscriberID
}

// NewManager creates a new logging service instance
func NewManager(bus events.Bus, core api.Core, logger *zap.Logger) *Manager {
	return &Manager{
		log:  logger,
		bus:  bus,
		core: core,
		config: api.Config{
			PropagateDownstream: true,
			StreamToEvents:      false,
			MinLevel:            zapcore.InfoLevel,
		},
		eventChan: make(chan events.Event),
	}
}

// Start initializes the service and starts listening for events
func (m *Manager) Start(ctx context.Context) error {
	// Subscribe to log configuration events and config requests
	subID, err := m.bus.SubscribeP(ctx, api.System, "*", m.eventChan)
	if err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}
	m.subID = subID

	// Apply initial configuration
	m.applyConfig(ctx, m.config)

	// Start event handling loop
	go m.eventLoop(ctx)

	m.log.Info("logging service started",
		zap.Bool("propagate", m.config.PropagateDownstream),
		zap.Bool("stream", m.config.StreamToEvents),
		zap.String("min_level", m.config.MinLevel.String()),
	)

	return nil
}

// Stop gracefully shuts down the service
func (m *Manager) Stop(ctx context.Context) error {
	if m.subID != "" {
		m.bus.Unsubscribe(ctx, m.subID)
		m.subID = ""
	}
	close(m.eventChan)
	m.log.Info("logging service stopped")
	return nil
}

// eventLoop handles incoming events
func (m *Manager) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-m.eventChan:
			if !ok {
				return
			}
			m.handleEvent(ctx, event)
		}
	}
}

// handleEvent processes incoming events
func (m *Manager) handleEvent(ctx context.Context, e events.Event) {
	switch e.Kind {
	case api.SetConfigEvent:
		m.handleConfigEvent(ctx, e)
	case api.GetConfigEvent:
		m.handleGetConfigEvent(ctx, e)
	}
}

// handleConfigEvent processes incoming log configuration events
func (m *Manager) handleConfigEvent(ctx context.Context, e events.Event) {
	cfg, ok := e.Data.(api.Config)
	if !ok {
		m.log.Error("invalid config data type")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for actual changes
	if m.config == cfg {
		return
	}

	m.log.Info("updating log configuration",
		zap.Bool("old_propagate", m.config.PropagateDownstream),
		zap.Bool("new_propagate", cfg.PropagateDownstream),
		zap.Bool("old_stream", m.config.StreamToEvents),
		zap.Bool("new_stream", cfg.StreamToEvents),
		zap.String("old_level", m.config.MinLevel.String()),
		zap.String("new_level", cfg.MinLevel.String()),
	)

	m.applyConfig(ctx, cfg)
}

// handleGetConfigEvent handles requests for current config state
func (m *Manager) handleGetConfigEvent(ctx context.Context, e events.Event) {
	req, ok := e.Data.(api.ConfigRequest)
	if !ok {
		m.log.Error("invalid config request data type")
		return
	}

	m.mu.RLock()
	currentConfig := m.config
	m.mu.RUnlock()

	// Send response with current config
	m.bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.ConfigStateEvent,
		Path:   req.ResponsePath,
		Data: api.ConfigResponse{
			Config: currentConfig,
		},
	})
}

// applyConfig applies a new logging configuration
func (m *Manager) applyConfig(ctx context.Context, cfg api.Config) {
	m.config = cfg
	m.core.Configure(cfg)
}

// GetConfig returns the current logging configuration
func (m *Manager) GetConfig() api.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}
