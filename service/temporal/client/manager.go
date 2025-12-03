package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/entry"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

// Manager handles Temporal client configuration and lifecycle
type Manager struct {
	log                *zap.Logger
	dtt                payload.Transcoder
	bus                event.Bus
	env                env.Registry
	factory            ClientFactory
	dataConverter      converter.DataConverter
	clientInterceptors []interceptor.ClientInterceptor

	mu       sync.RWMutex
	configs  map[registry.ID]*api.ClientConfig
	services map[registry.ID]*Client
}

// NewManager creates a new client manager instance
func NewManager(
	logger *zap.Logger,
	transcoder payload.Transcoder,
	bus event.Bus,
	envRegistry env.Registry,
	dataConverter converter.DataConverter,
	clientInterceptors []interceptor.ClientInterceptor,
) (*Manager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if transcoder == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if envRegistry == nil {
		return nil, fmt.Errorf("env registry is required")
	}

	factory := NewDefaultClientFactory(envRegistry, dataConverter, clientInterceptors)

	return &Manager{
		log:                logger,
		dtt:                transcoder,
		bus:                bus,
		env:                envRegistry,
		factory:            factory,
		dataConverter:      dataConverter,
		clientInterceptors: clientInterceptors,
		configs:            make(map[registry.ID]*api.ClientConfig),
		services:           make(map[registry.ID]*Client),
	}, nil
}

// NewManagerWithFactory creates a new client manager with a custom factory (for testing)
func NewManagerWithFactory(
	logger *zap.Logger,
	transcoder payload.Transcoder,
	bus event.Bus,
	envRegistry env.Registry,
	factory ClientFactory,
	dataConverter converter.DataConverter,
	clientInterceptors []interceptor.ClientInterceptor,
) (*Manager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if transcoder == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if envRegistry == nil {
		return nil, fmt.Errorf("env registry is required")
	}
	if factory == nil {
		return nil, fmt.Errorf("factory is required")
	}

	return &Manager{
		log:                logger,
		dtt:                transcoder,
		bus:                bus,
		env:                envRegistry,
		factory:            factory,
		dataConverter:      dataConverter,
		clientInterceptors: clientInterceptors,
		configs:            make(map[registry.ID]*api.ClientConfig),
		services:           make(map[registry.ID]*Client),
	}, nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("processing temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", string(ent.Kind)))

	cfg, err := entry.DecodeEntryConfig[api.ClientConfig](ctx, m.dtt, ent)
	if err != nil {
		return fmt.Errorf("failed to decode client config: %w", err)
	}

	return m.AddClient(ctx, ent.ID, cfg)
}

// AddClient initializes a new client instance with the given configuration
func (m *Manager) AddClient(ctx context.Context, id registry.ID, cfg *api.ClientConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if client already exists
	if _, exists := m.services[id]; exists {
		return fmt.Errorf("client %s already initialized", id)
	}

	if _, exists := m.configs[id]; exists {
		return fmt.Errorf("client config %s already exists", id)
	}

	// Initialize defaults
	cfg.InitDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	// Store configuration
	m.configs[id] = cfg

	// Create new service
	service, err := m.factory.CreateClient(ctx, m.log.With(zap.String("id", id.String())), id, cfg)
	if err != nil {
		delete(m.configs, id)
		return fmt.Errorf("failed to create client: %w", err)
	}

	m.services[id] = service

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: service,
			Config:  cfg.Lifecycle,
		},
	})

	// Register as resource provider
	meta := attrs.NewBag()
	meta.Set("type", "temporal.client")

	m.bus.Send(ctx, event.Event{
		System: resource.System,
		Kind:   resource.Register,
		Path:   id.String(),
		Data: resource.Entry{
			ID:       id,
			Provider: service,
			Meta:     meta,
		},
	})

	m.log.Info("initialized temporal client",
		zap.String("id", id.String()),
		zap.String("address", cfg.Address),
		zap.String("namespace", cfg.Namespace),
		zap.String("auth_type", string(cfg.Auth.Type)),
		zap.String("tq_prefix", cfg.TQPrefix))

	return nil
}

// Update implements registry.EntryListener
func (m *Manager) Update(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("updating temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", string(ent.Kind)))

	cfg, err := entry.DecodeEntryConfig[api.ClientConfig](ctx, m.dtt, ent)
	if err != nil {
		return fmt.Errorf("failed to decode client config: %w", err)
	}

	return m.UpdateClient(ctx, ent.ID, cfg)
}

// UpdateClient updates an existing client configuration
func (m *Manager) UpdateClient(ctx context.Context, id registry.ID, cfg *api.ClientConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.configs[id]; !exists {
		return fmt.Errorf("client config %s not found", id)
	}

	// Initialize defaults
	cfg.InitDefaults()

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid client config: %w", err)
	}

	// Store updated configuration
	m.configs[id] = cfg

	// Update supervisor configuration
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	m.log.Info("updated temporal client config",
		zap.String("id", id.String()),
		zap.String("tq_prefix", cfg.TQPrefix))

	return nil
}

// Delete implements registry.EntryListener
func (m *Manager) Delete(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.KindClient {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("deleting temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", string(ent.Kind)))

	return m.DeleteClient(ctx, ent.ID)
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
		Kind:   supervisor.ServiceRemove,
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

	m.log.Info("deleted temporal client", zap.String("id", id.String()))
	return nil
}

// GetClient retrieves an existing client by id
func (m *Manager) GetClient(id registry.ID) (*Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	service, exists := m.services[id]
	if !exists {
		return nil, fmt.Errorf("client %s not initialized", id)
	}
	return service, nil
}

// GetConfig retrieves a client config by id
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
