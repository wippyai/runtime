package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/config"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

// Manager handles Temporal client configuration and lifecycle
type Manager struct {
	log     *zap.Logger
	dtt     payload.Transcoder
	bus     event.Bus
	factory ClientFactory
	dc      converter.DataConverter

	mu       sync.RWMutex
	configs  map[registry.ID]*api.ClientConfig
	services map[registry.ID]*Client
}

// NewClientManager creates a new client manager instance
func NewClientManager(logger *zap.Logger) *Manager {
	return NewClientManagerWithFactory(logger, NewDefaultClientFactory(), nil)
}

// NewClientManagerWithFactory creates a new client manager with a custom factory
func NewClientManagerWithFactory(logger *zap.Logger, factory ClientFactory, dc converter.DataConverter) *Manager {
	return &Manager{
		log:      logger,
		factory:  factory,
		dc:       dc,
		configs:  make(map[registry.ID]*api.ClientConfig),
		services: make(map[registry.ID]*Client),
	}
}

// Start initializes the manager with required dependencies
func (m *Manager) Start(ctx context.Context) error {
	m.bus = event.GetBus(ctx)
	m.dtt = payload.GetTranscoder(ctx)

	if m.bus == nil {
		return fmt.Errorf("event bus is required")
	}

	if m.dtt == nil {
		return fmt.Errorf("transcoder is required")
	}

	return nil
}

// Stop cleans up any resources used by the manager
func (m *Manager) Stop() error {
	return nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", entry.Kind)
	}

	// Make sure transcoder is initialized
	if m.dtt == nil {
		m.dtt = payload.GetTranscoder(ctx)
		if m.dtt == nil {
			return fmt.Errorf("transcoder is not available, service might not be fully initialized")
		}
	}

	// Make sure the bus is initialized
	if m.bus == nil {
		m.bus = event.GetBus(ctx)
		if m.bus == nil {
			return fmt.Errorf("event bus is not available, service might not be fully initialized")
		}
	}

	// Print debug information
	m.log.Debug("processing temporal client entry",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	cfg, err := config.DecodeAndInitConfig[api.ClientConfig](m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode client config: %w", err)
	}

	return m.AddClient(ctx, entry.ID, cfg, m.dc)
}

// AddClient initializes a new client instance with the given configuration
func (m *Manager) AddClient(ctx context.Context, id registry.ID, cfg *api.ClientConfig, dc converter.DataConverter) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if client already exists
	if _, exists := m.services[id]; exists {
		return fmt.Errorf("client %s already initialized", id)
	}

	if _, exists := m.configs[id]; exists {
		return fmt.Errorf("client config %s already exists", id)
	}

	// Set defaults if needed
	if cfg.Auth.Type == "" {
		cfg.Auth.Type = api.AuthTypeNone
	}

	// Store configuration
	m.configs[id] = cfg

	// Create new service
	service, err := m.factory.CreateClient(m.log, id, dc, cfg)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	m.services[id] = service

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: service,
			Config:  cfg.Lifecycle,
		},
	})

	// Register as resource provider
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data: resource.Entry{
			ID:       id,
			Provider: service,
			Meta:     map[string]interface{}{"type": "temporal.client"},
		},
	})

	m.log.Info("initialized client",
		zap.String("id", id.String()),
		zap.String("address", cfg.Connect.Address),
		zap.String("namespace", cfg.Connect.Namespace),
		zap.String("auth_type", string(cfg.Auth.Type)),
		zap.String("tq_prefix", cfg.TQPrefix),
	)

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", entry.Kind)
	}

	// Make sure transcoder is initialized
	if m.dtt == nil {
		m.dtt = payload.GetTranscoder(ctx)
		if m.dtt == nil {
			return fmt.Errorf("transcoder is not available, service might not be fully initialized")
		}
	}

	// Make sure the bus is initialized
	if m.bus == nil {
		m.bus = event.GetBus(ctx)
		if m.bus == nil {
			return fmt.Errorf("event bus is not available, service might not be fully initialized")
		}
	}

	m.log.Debug("updating temporal client entry",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	cfg, err := config.DecodeAndInitConfig[api.ClientConfig](m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode client config: %w", err)
	}

	return m.UpdateClient(ctx, entry.ID, cfg)
}

// UpdateClient updates an existing client configuration
func (m *Manager) UpdateClient(ctx context.Context, id registry.ID, config *api.ClientConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client config %s not found", id)
	}

	// Store updated configuration
	m.configs[id] = config

	// Update supervisor configuration
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Config: config.Lifecycle,
		},
	})

	// Note: Changes won't take effect until the client is restarted
	m.log.Info("updated client config",
		zap.String("id", id.String()),
		zap.String("tq_prefix", config.TQPrefix))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", entry.Kind)
	}

	// Make sure bus is initialized
	if m.bus == nil {
		m.bus = event.GetBus(ctx)
		if m.bus == nil {
			return fmt.Errorf("event bus is not available, service might not be fully initialized")
		}
	}

	m.log.Debug("deleting temporal client entry",
		zap.String("id", entry.ID.String()),
		zap.String("kind", string(entry.Kind)))

	return m.DeleteClient(ctx, entry.ID)
}

// DeleteClient removes a client configuration and service if it exists
func (m *Manager) DeleteClient(ctx context.Context, id registry.ID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client config %s not found", id)
	}

	// Unregister from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   id.String(),
	})

	// Unregister from resource system
	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Delete,
		Path:   id.String(),
		Data:   id,
	})

	delete(m.configs, id)
	delete(m.services, id)

	m.log.Info("deleted client", zap.String("id", id.String()))
	return nil
}

// GetClient retrieves an existing client by ID
func (m *Manager) GetClient(id registry.ID) (*Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	service, exists := m.services[id]
	if !exists {
		return nil, fmt.Errorf("client %s not initialized", id)
	}
	return service, nil
}

// GetConfig retrieves a client config by ID
func (m *Manager) GetConfig(id registry.ID) (*api.ClientConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	cfg, exists := m.configs[id]
	return cfg, exists
}

// Has checks if a client config exists
func (m *Manager) Has(id registry.ID) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.configs[id]
	return exists
}

// GetTaskQueueName applies the client's prefix to a task queue name
func (m *Manager) GetTaskQueueName(clientID registry.ID, queueName string) (string, error) {
	client, err := m.GetClient(clientID)
	if err != nil {
		return "", err
	}
	return client.GetTaskQueueName(queueName), nil
}
