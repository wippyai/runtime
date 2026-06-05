// SPDX-License-Identifier: MPL-2.0

package logs

import (
	"context"
	"sync"

	api "github.com/wippyai/runtime/api/logs"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// Manager manages logging configuration and event handling. This is considered to be a root service since it trunks
// all the logs from the system and sends them to the event bus. Unmanaged.
type Manager struct {
	bus    event.Bus
	core   api.Core
	ctx    context.Context
	log    *zap.Logger
	sub    *eventbus.Subscriber
	mu     sync.RWMutex
	config api.Config
}

const configEventPattern = "logs.config.(set|get)"

// NewManager creates a new logging service instance.
func NewManager(
	bus event.Bus,
	core api.Core,
	logger *zap.Logger,
	config api.Config,
) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		log:    logger,
		bus:    bus,
		core:   core,
		config: config,
	}
}

// Start initializes the service and starts listening for events
func (m *Manager) Start(ctx context.Context) error {
	// Subscribe to log configuration events and config requests
	sub, err := eventbus.NewSubscriber(ctx, m.bus, api.System, configEventPattern, m.handleEvent)
	if err != nil {
		return NewSubscriberError(err)
	}
	m.sub = sub

	m.ctx = ctx

	// Apply initial configuration
	m.handleSetConfigEvent(ctx, "start", m.config)

	m.log.Info("logging service started",
		zap.Bool("propagate", m.config.PropagateDownstream),
		zap.Bool("stream", m.config.StreamToEvents),
	)

	return nil
}

// Stop gracefully shuts down the service
func (m *Manager) Stop() error {
	if m.sub != nil {
		m.sub.Close()
		m.sub = nil
	}
	m.log.Info("logging service stopped")
	return nil
}

// handleEvent processes incoming events
func (m *Manager) handleEvent(e event.Event) {
	switch e.Kind {
	case api.SetConfig:
		m.handleConfigEvent(m.ctx, e)
	case api.GetConfig:
		m.handleGetConfigEvent(m.ctx, e)
	}
}

// handleConfigEvent processes incoming log configuration events
func (m *Manager) handleConfigEvent(ctx context.Context, e event.Event) {
	cfg, ok := e.Data.(api.Config)
	if !ok {
		m.log.Error("invalid config data type")
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for actual changes
	if m.config == cfg {
		// Even if config hasn't changed, send confirmation to prevent timeouts.
		m.sendConfigState(ctx, e.Path, cfg)
		return
	}

	m.log.Info("updating log configuration",
		zap.Bool("old_propagate", m.config.PropagateDownstream),
		zap.Bool("new_propagate", cfg.PropagateDownstream),
		zap.Bool("old_stream", m.config.StreamToEvents),
		zap.Bool("new_stream", cfg.StreamToEvents),
	)

	m.handleSetConfigEvent(ctx, e.Path, cfg)
}

// handleGetConfigEvent handles requests for current config state
func (m *Manager) handleGetConfigEvent(ctx context.Context, e event.Event) {
	m.mu.RLock()
	currentConfig := m.config
	m.mu.RUnlock()

	m.sendConfigState(ctx, e.Path, currentConfig)
}

// handleSetConfigEvent applies a new logging configuration
func (m *Manager) handleSetConfigEvent(ctx context.Context, path event.Path, cfg api.Config) {
	m.config = cfg
	m.core.Configure(cfg)
	m.sendConfigState(ctx, path, cfg)
}

func (m *Manager) sendConfigState(ctx context.Context, path event.Path, cfg api.Config) {
	m.bus.Send(ctx, event.Event{
		System: api.System,
		Kind:   api.ConfigState,
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

func (m *Manager) SetCollector(c metrics.Collector) {
	m.core.SetCollector(c)
}
