package logs

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	api "github.com/ponyruntime/pony/api/service/logs"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Manager manages logging configuration and event handling. This is considered to be a root service since it trunks
// all the logs from the system and sends them to the event bus. Unmanaged.
type Manager struct {
	log    *zap.Logger
	bus    events.Bus
	core   api.Core
	mu     sync.RWMutex
	config api.Config
	ctx    context.Context
	sub    *eventbus.Subscriber
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
			MinLevel:            zapcore.InfoLevel, // todo: pass from outside
		},
	}
}

// Start initializes the service and starts listening for events
func (m *Manager) Start(ctx context.Context) error {
	// Subscribe to log configuration events and config requests
	sub, err := eventbus.NewSubscriber(ctx, m.bus, api.System, "logs.config.(set|get)", m.handleEvent)
	if err != nil {
		return fmt.Errorf("failed to subscribe to events: %w", err)
	}
	m.sub = sub

	m.ctx = ctx

	// Apply initial configuration
	m.handleSetConfigEvent(ctx, "start", m.config)

	m.log.Info("logging service started",
		zap.Bool("propagate", m.config.PropagateDownstream),
		zap.Bool("stream", m.config.StreamToEvents),
		zap.String("min_level", m.config.MinLevel.String()),
	)

	return nil
}

// Stop gracefully shuts down the service
func (m *Manager) Stop(context.Context) error {
	if m.sub != nil {
		m.sub.Close()
		m.sub = nil
	}
	m.log.Info("logging service stopped")
	return nil
}

// handleEvent processes incoming events
func (m *Manager) handleEvent(e events.Event) {
	switch e.Kind {
	case api.SetConfigEvent:
		m.handleConfigEvent(m.ctx, e)
	case api.GetConfigEvent:
		m.handleGetConfigEvent(m.ctx, e)
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

	m.handleSetConfigEvent(ctx, e.Path, cfg)
}

// handleGetConfigEvent handles requests for current config state
func (m *Manager) handleGetConfigEvent(ctx context.Context, e events.Event) {
	m.mu.RLock()
	currentConfig := m.config
	m.mu.RUnlock()

	// Send response with current config
	m.bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.ConfigStateEvent,
		Path:   e.Path,
		Data:   currentConfig,
	})
}

// handleSetConfigEvent applies a new logging configuration
func (m *Manager) handleSetConfigEvent(ctx context.Context, path events.Path, cfg api.Config) {
	m.config = cfg
	m.core.Configure(cfg)
	// Send confirmation that config was applied
	m.bus.Send(ctx, events.Event{
		System: api.System,
		Kind:   api.ConfigStateEvent,
		Path:   path,
		Data:   cfg,
	})
}

// GetConfig returns the current logging configuration
func (m *Manager) GetConfig() api.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}
