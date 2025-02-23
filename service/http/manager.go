package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
	"net/http"
	"sync"
)

// ServerFactoryAPI creates new server instances
type ServerFactoryAPI interface {
	CreateServer(cfg *config.ServerConfig) (Server, error)
}

// EndpointFactoryAPI defines the interface for creating endpoint handlers
type EndpointFactoryAPI interface {
	// CreateHandler creates a new endpoint handler from the given configuration
	CreateHandler(ctx context.Context, cfg *config.EndpointConfig) (http.Handler, error)
}

// StaticFactoryAPI defines the interface for creating static file handlers
type StaticFactoryAPI interface {
	// CreateHandler creates a new static file handler from the given configuration
	CreateHandler(ctx context.Context, cfg *config.StaticConfig) (http.Handler, error)
}

// Server represents an HTTP server with routing capabilities
type Server interface {
	supervisor.Service

	UpdateConfig(cfg *config.ServerConfig) error

	AddRouter(id registry.ID, router *config.RouterConfig) error
	DeleteRouter(id registry.ID) error
	AddEndpoint(routerID, id registry.ID, path string, method string, handler http.Handler) error
	RemoveEndpoint(routerID, id registry.ID) error

	Mount(id registry.ID, path string, handler http.Handler) error
	Remove(id registry.ID) error
	Rebuild(ctx context.Context) error
}

type Manager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus events.Bus

	serverFactory   ServerFactoryAPI
	endpointFactory EndpointFactoryAPI
	staticFactory   StaticFactoryAPI

	mu            sync.Mutex
	servers       map[registry.ID]Server
	routerServers map[registry.ID]registry.ID // router ID -> server ID mapping
	pending       map[registry.ID]bool
}

func NewManager(
	dtt payload.Transcoder,
	bus events.Bus,
	serverFactory ServerFactoryAPI,
	endpointFactory EndpointFactoryAPI,
	staticFactory StaticFactoryAPI,
	log *zap.Logger,
) (*Manager, error) {
	if dtt == nil {
		return nil, fmt.Errorf("transcoder is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if serverFactory == nil {
		return nil, fmt.Errorf("server factory is required")
	}
	if endpointFactory == nil {
		return nil, fmt.Errorf("endpoint factory is required")
	}
	if staticFactory == nil {
		return nil, fmt.Errorf("static factory is required")
	}

	return &Manager{
		log:             log,
		dtt:             dtt,
		bus:             bus,
		serverFactory:   serverFactory,
		endpointFactory: endpointFactory,
		staticFactory:   staticFactory,
		servers:         make(map[registry.ID]Server),
		routerServers:   make(map[registry.ID]registry.ID),
		pending:         make(map[registry.ID]bool),
	}, nil
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.KindServer:
		return m.handleServerAdd(ctx, entry)
	case config.KindRouter:
		return m.handleRouterAdd(ctx, entry)
	case config.KindEndpoint:
		return m.handleEndpointAdd(ctx, entry)
	case config.KindStatic:
		return m.handleStaticAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.KindServer:
		return m.handleServerUpdate(ctx, entry)
	case config.KindRouter:
		return m.handleRouterUpdate(ctx, entry)
	case config.KindEndpoint:
		return m.handleEndpointAdd(ctx, entry)
	case config.KindStatic:
		return m.handleStaticAdd(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.KindServer:
		return m.handleServerDelete(ctx, entry)
	case config.KindRouter:
		return m.handleRouterDelete(ctx, entry)
	case config.KindEndpoint, config.KindStatic:
		return m.handleHandlerDelete(ctx, entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

func (m *Manager) handleServerAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.ServerConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	server, err := m.serverFactory.CreateServer(cfg)
	if err != nil {
		return err
	}

	if _, exists := m.servers[entry.ID]; exists {
		return fmt.Errorf("server %s already exists", entry.ID)
	}

	m.servers[entry.ID] = server

	// Register with supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: server, Config: cfg.Lifecycle},
	})

	return nil
}

func (m *Manager) handleServerUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.ServerConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	server, exists := m.servers[entry.ID]
	if !exists {
		return fmt.Errorf("server %s not found", entry.ID)
	}

	if err := server.UpdateConfig(cfg); err != nil {
		return fmt.Errorf("failed to update server config: %w", err)
	}
	m.pending[entry.ID] = true

	// Update supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	return nil
}

func (m *Manager) handleServerDelete(ctx context.Context, entry registry.Entry) error {
	_, exists := m.servers[entry.ID]
	if !exists {
		return fmt.Errorf("server %s not found", entry.ID)
	}

	// Remove from supervisor
	m.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Clean up router mappings
	for routerID, serverID := range m.routerServers {
		if serverID == entry.ID {
			delete(m.routerServers, routerID)
		}
	}

	delete(m.servers, entry.ID)
	delete(m.pending, entry.ID)
	return nil
}

func (m *Manager) handleRouterAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.RouterConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	serverID := registry.ParseID(cfg.Meta.StringValue(config.ServerID)).WithDefaultNS(entry.ID.NS)
	server, exists := m.servers[serverID]
	if !exists {
		return fmt.Errorf("server %s not found", serverID)
	}

	if err := server.AddRouter(entry.ID, cfg); err != nil {
		return err
	}

	m.routerServers[entry.ID] = serverID
	m.pending[serverID] = true
	return nil
}

func (m *Manager) handleRouterUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.RouterConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	// Get current server for this router
	currentServerID, exists := m.routerServers[entry.ID]
	if !exists {
		return fmt.Errorf("router %s not found", entry.ID)
	}

	// Get target server from updated config
	newServerID := registry.ParseID(cfg.Meta.StringValue(config.ServerID)).WithDefaultNS(entry.ID.NS)
	newServer, exists := m.servers[newServerID]
	if !exists {
		return fmt.Errorf("target server %s not found", newServerID)
	}

	// If server changed, delete from old and add to new
	if currentServerID != newServerID {
		if oldServer, exists := m.servers[currentServerID]; exists {
			if err := oldServer.DeleteRouter(entry.ID); err != nil {
				return err
			}
		}
		if err := newServer.AddRouter(entry.ID, cfg); err != nil {
			return err
		}
		m.routerServers[entry.ID] = newServerID
		m.pending[newServerID] = true
	} else {
		// Update existing router
		if err := newServer.AddRouter(entry.ID, cfg); err != nil {
			return err
		}
		m.pending[currentServerID] = true
	}

	return nil
}

func (m *Manager) handleRouterDelete(ctx context.Context, entry registry.Entry) error {
	serverID, exists := m.routerServers[entry.ID]
	if !exists {
		return fmt.Errorf("router %s not found", entry.ID)
	}

	server, exists := m.servers[serverID]
	if !exists {
		return fmt.Errorf("server %s not found", serverID)
	}

	if err := server.DeleteRouter(entry.ID); err != nil {
		return err
	}

	delete(m.routerServers, entry.ID)
	m.pending[serverID] = true
	return nil
}

func (m *Manager) handleEndpointAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.EndpointConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	routerID := registry.ParseID(cfg.Meta.StringValue(config.RouterID)).WithDefaultNS(entry.ID.NS)
	serverID, exists := m.routerServers[routerID]
	if !exists {
		return fmt.Errorf("router %s not found", routerID)
	}

	server, exists := m.servers[serverID]
	if !exists {
		return fmt.Errorf("server %s not found", serverID)
	}

	handler, err := m.endpointFactory.CreateHandler(ctx, cfg)
	if err != nil {
		return err
	}

	if err := server.AddEndpoint(routerID, entry.ID, cfg.Path, cfg.Method, handler); err != nil {
		return err
	}

	m.pending[serverID] = true
	return nil
}

func (m *Manager) handleStaticAdd(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.StaticConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	serverID := registry.ParseID(cfg.Meta.StringValue(config.ServerID)).WithDefaultNS(entry.ID.NS)
	server, exists := m.servers[serverID]
	if !exists {
		return fmt.Errorf("server %s not found", serverID)
	}

	handler, err := m.staticFactory.CreateHandler(ctx, cfg)
	if err != nil {
		return err
	}

	if err := server.Mount(entry.ID, cfg.Path, handler); err != nil {
		return err
	}

	m.pending[serverID] = true
	return nil
}

func (m *Manager) handleHandlerDelete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case config.KindEndpoint:
		cfg, err := decodeEntity[config.EndpointConfig](entry, m.dtt)
		if err != nil {
			return err
		}
		routerID := registry.ParseID(cfg.Meta.StringValue(config.RouterID)).WithDefaultNS(entry.ID.NS)
		serverID, exists := m.routerServers[routerID]
		if !exists {
			return fmt.Errorf("router %s not found", routerID)
		}

		server, exists := m.servers[serverID]
		if !exists {
			return fmt.Errorf("server %s not found", serverID)
		}

		if err := server.RemoveEndpoint(routerID, entry.ID); err != nil {
			return err
		}

		m.pending[serverID] = true

	case config.KindStatic:
		cfg, err := decodeEntity[config.StaticConfig](entry, m.dtt)
		if err != nil {
			return err
		}
		serverID := registry.ParseID(cfg.Meta.StringValue(config.ServerID)).WithDefaultNS(entry.ID.NS)
		server, exists := m.servers[serverID]
		if !exists {
			return fmt.Errorf("server %s not found", serverID)
		}

		if err := server.Remove(entry.ID); err != nil {
			return err
		}

		m.pending[serverID] = true
	}

	return nil
}

func (m *Manager) Begin(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[registry.ID]bool)
}

func (m *Manager) Commit(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for serverID := range m.pending {
		if server, exists := m.servers[serverID]; exists {
			if err := server.Rebuild(ctx); err != nil {
				m.log.Error("failed to rebuild router",
					zap.String("server", serverID.String()),
					zap.Error(err))
			}
		}
	}

	m.pending = make(map[registry.ID]bool)
}

func (m *Manager) Discard(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[registry.ID]bool)
}

// decodeEntity is a helper to decode registry entries into specific configs
func decodeEntity[T any](entry registry.Entry, transcoder payload.Transcoder) (*T, error) {
	if entry.Data == nil {
		return nil, fmt.Errorf("configuration data is required")
	}

	cfg := new(T)
	if err := transcoder.Unmarshal(entry.Data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate if the config implements Validate()
	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return cfg, nil
}
