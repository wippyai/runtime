package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/entry"
	"github.com/wippyai/runtime/service/temporal/peer"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/interceptor"
	"go.uber.org/zap"
)

var _ registry.EntryListener = (*Manager)(nil)

// Manager handles Temporal client configuration and lifecycle
type Manager struct {
	log                *zap.Logger
	dtt                payload.Transcoder
	bus                event.Bus
	env                env.Registry
	factory            Factory
	dataConverter      converter.DataConverter
	clientInterceptors []interceptor.ClientInterceptor

	mu       sync.RWMutex
	configs  map[registry.ID]*api.ClientConfig
	services map[registry.ID]*Client
	peers    map[registry.ID]*peer.Receiver
}

// ManagerOption configures a Manager instance
type ManagerOption func(*Manager)

// WithLogger sets the logger for the Manager
func WithLogger(logger *zap.Logger) ManagerOption {
	return func(m *Manager) {
		m.log = logger
	}
}

// WithTranscoder sets the payload transcoder for the Manager
func WithTranscoder(transcoder payload.Transcoder) ManagerOption {
	return func(m *Manager) {
		m.dtt = transcoder
	}
}

// WithEventBus sets the event bus for the Manager
func WithEventBus(bus event.Bus) ManagerOption {
	return func(m *Manager) {
		m.bus = bus
	}
}

// WithEnvRegistry sets the environment registry for the Manager
func WithEnvRegistry(reg env.Registry) ManagerOption {
	return func(m *Manager) {
		m.env = reg
	}
}

// WithDataConverter sets the data converter for the Manager
func WithDataConverter(dc converter.DataConverter) ManagerOption {
	return func(m *Manager) {
		m.dataConverter = dc
	}
}

// WithInterceptors sets the client interceptors for the Manager
func WithInterceptors(interceptors []interceptor.ClientInterceptor) ManagerOption {
	return func(m *Manager) {
		m.clientInterceptors = interceptors
	}
}

// WithFactory sets a custom client factory for the Manager
func WithFactory(factory Factory) ManagerOption {
	return func(m *Manager) {
		m.factory = factory
	}
}

// NewManager creates a new client manager instance with functional options
func NewManager(opts ...ManagerOption) (*Manager, error) {
	m := &Manager{
		configs:  make(map[registry.ID]*api.ClientConfig),
		services: make(map[registry.ID]*Client),
		peers:    make(map[registry.ID]*peer.Receiver),
	}

	for _, opt := range opts {
		opt(m)
	}

	if m.log == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if m.dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if m.bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if m.env == nil {
		return nil, fmt.Errorf("env registry is required")
	}

	if m.factory == nil {
		m.factory = NewDefaultClientFactory(m.env, m.dataConverter, m.clientInterceptors)
	}

	return m, nil
}

// Add implements registry.EntryListener
func (m *Manager) Add(ctx context.Context, ent registry.Entry) error {
	if ent.Kind != api.Client {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("processing temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

	cfg, err := entry.DecodeEntryConfig[api.ClientConfig](ctx, m.dtt, ent)
	if err != nil {
		return fmt.Errorf("failed to decode client config: %w", err)
	}

	return m.AddClient(ctx, ent.ID, cfg)
}

// AddClient initializes a new client instance with the given configuration
func (m *Manager) AddClient(ctx context.Context, id registry.ID, cfg *api.ClientConfig) error {
	// Check existence under lock
	m.mu.Lock()
	if _, exists := m.services[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("client %s already initialized", id)
	}
	if _, exists := m.configs[id]; exists {
		m.mu.Unlock()
		return fmt.Errorf("client config %s already exists", id)
	}

	// Initialize and validate
	cfg.InitDefaults()
	if err := cfg.Validate(); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("invalid client config: %w", err)
	}

	// Mark as pending to prevent concurrent adds
	m.configs[id] = cfg
	m.mu.Unlock()

	// Create client without holding lock (may involve network I/O)
	service, err := m.factory.CreateClient(ctx, m.log.With(zap.String("id", id.String())), id, cfg)
	if err != nil {
		m.mu.Lock()
		delete(m.configs, id)
		m.mu.Unlock()
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Store service under lock, verify config still exists (not deleted by concurrent op)
	m.mu.Lock()
	if _, exists := m.configs[id]; !exists {
		m.mu.Unlock()
		// Config was deleted while we were creating - cleanup and fail
		if err := service.Stop(ctx); err != nil {
			m.log.Warn("failed to stop orphaned client during cleanup",
				zap.String("id", id.String()),
				zap.Error(err))
		}
		return fmt.Errorf("client %s was removed during initialization", id)
	}
	m.services[id] = service

	// Create peer receiver
	var peerReceiver *peer.Receiver
	router := relay.GetRouter(ctx)
	if router != nil {
		nodeID := pid.NodeID(id.String())
		peerReceiver = peer.NewReceiver(ctx, nodeID, service.TemporalClient(), router, m.log.Named("peer"))
		m.peers[id] = peerReceiver
	}
	m.mu.Unlock()

	// Send events without holding lock to prevent deadlock
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   id.String(),
		Data: &supervisor.Entry{
			Service: service,
			Config:  cfg.Lifecycle,
		},
	})

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

	if peerReceiver != nil {
		m.bus.Send(ctx, event.Event{
			System: relay.System,
			Kind:   relay.PeerRegister,
			Path:   id.String(),
			Data: relay.PeerInfo{
				NodeID:   pid.NodeID(id.String()),
				Receiver: peerReceiver,
			},
		})
	}

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
	if ent.Kind != api.Client {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("updating temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

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
	if ent.Kind != api.Client {
		return fmt.Errorf("unexpected entry kind: %s", ent.Kind)
	}

	m.log.Debug("deleting temporal client entry",
		zap.String("id", ent.ID.String()),
		zap.String("kind", ent.Kind))

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

	// Stop and unregister peer receiver
	if peerReceiver, exists := m.peers[id]; exists {
		peerReceiver.Stop()
		m.bus.Send(ctx, event.Event{
			System: relay.System,
			Kind:   relay.PeerDelete,
			Path:   id.String(),
		})
		delete(m.peers, id)
	}

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
