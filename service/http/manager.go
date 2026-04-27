// SPDX-License-Identifier: MPL-2.0

package http

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

// ServerFactoryAPI creates new server instances
type ServerFactoryAPI interface {
	// CreateServer creates a new HTTP server from the provided configuration
	CreateServer(id registry.ID, cfg *config.ServerConfig) (Server, error)
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
	relay.Receiver

	// UpdateConfig updates the server configuration
	UpdateConfig(cfg *config.ServerConfig) error

	// UpsertRouter adds a new router or updates an existing one
	UpsertRouter(id registry.ID, router *config.RouterConfig) error

	// DeleteRouter removes a router by Source
	DeleteRouter(id registry.ID) error

	// UpsertEndpoint adds a new endpoint or updates an existing one
	UpsertEndpoint(routerID, id registry.ID, path string, method string, handler http.Handler) error

	// RemoveEndpoint removes an endpoint from a router
	RemoveEndpoint(routerID, id registry.ID) error

	// Mount adds a handler at a specific path
	Mount(id registry.ID, path string, handler http.Handler) error

	// Remove unmounts a handler by Source
	Remove(id registry.ID) error

	// Rebuild rebuilds the server's routing configuration
	Rebuild(ctx context.Context) error
}

// Manager coordinates HTTP servers, routers, endpoints, and static file handlers
type Manager struct {
	dtt             payload.Transcoder
	bus             event.Bus
	serverFactory   ServerFactoryAPI
	endpointFactory EndpointFactoryAPI
	staticFactory   StaticFactoryAPI
	log             *zap.Logger
	servers         map[registry.ID]Server
	routerServers   map[registry.ID]registry.ID
	endpointRouters map[registry.ID]registry.ID
	staticServers   map[registry.ID]registry.ID
	pending         map[registry.ID]bool
	mu              sync.Mutex
}

// NewManager creates a new HTTP service manager
func NewManager(
	dtt payload.Transcoder,
	bus event.Bus,
	serverFactory ServerFactoryAPI,
	endpointFactory EndpointFactoryAPI,
	staticFactory StaticFactoryAPI,
	log *zap.Logger,
) (*Manager, error) {
	if dtt == nil {
		return nil, ErrTranscoderRequired
	}
	if bus == nil {
		return nil, ErrEventBusRequired
	}
	if serverFactory == nil {
		return nil, ErrServerFactoryRequired
	}
	if endpointFactory == nil {
		return nil, ErrEndpointFactoryRequired
	}
	if staticFactory == nil {
		return nil, ErrStaticFactoryRequired
	}
	if log == nil {
		log = zap.NewNop()
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
		endpointRouters: make(map[registry.ID]registry.ID),
		staticServers:   make(map[registry.ID]registry.ID),
		pending:         make(map[registry.ID]bool),
	}, nil
}

//
// Public API
//

// Add adds a new HTTP component (server, router, endpoint, or static handler)
func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.Server:
		return m.handleServerCreate(ctx, entry)
	case config.Router:
		return m.handleRouterCreate(ctx, entry)
	case config.Endpoint:
		return m.handleEndpointUpsert(ctx, entry)
	case config.Static:
		return m.handleStaticUpsert(ctx, entry)
	default:
		m.log.Warn("Unsupported entry kind in HTTP Manager", zap.String("kind", entry.Kind), zap.String("id", entry.ID.String()))
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Update updates an existing HTTP component
func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.Server:
		return m.handleServerUpdate(ctx, entry)
	case config.Router:
		return m.handleRouterUpdate(ctx, entry)
	case config.Endpoint:
		return m.handleEndpointUpsert(ctx, entry)
	case config.Static:
		return m.handleStaticUpsert(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Delete removes an HTTP component
func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch entry.Kind {
	case config.Server:
		return m.handleServerDelete(ctx, entry)
	case config.Router:
		return m.handleRouterDelete(ctx, entry)
	case config.Endpoint:
		return m.handleEndpointDelete(ctx, entry)
	case config.Static:
		return m.handleStaticDelete(ctx, entry)
	default:
		return NewUnsupportedEntryKindError(entry.Kind)
	}
}

// Begin starts a transaction for applying changes
// This clears any pending rebuilds
func (m *Manager) Begin(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[registry.ID]bool)
	return nil
}

// Commit applies all pending changes to the servers
// Rebuilds any servers that have been modified
func (m *Manager) Commit(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.pending) == 0 {
		return nil
	}

	failed := make(map[registry.ID]bool)
	var errs []error
	for serverID := range m.pending {
		if server, exists := m.servers[serverID]; exists {
			if err := server.Rebuild(ctx); err != nil {
				m.log.Error("failed to rebuild router",
					zap.String("server", serverID.String()),
					zap.Error(err))
				failed[serverID] = true
				errs = append(errs, err)
			}
		}
	}

	m.pending = failed
	return errors.Join(errs...)
}

// Discard cancels any pending changes without applying them
func (m *Manager) Discard(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[registry.ID]bool)
	return nil
}

//
// Server handlers
//

// handleServerCreate creates a new HTTP server
func (m *Manager) handleServerCreate(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.ServerConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	server, err := m.serverFactory.CreateServer(entry.ID, cfg)
	if err != nil {
		return err
	}

	if _, exists := m.servers[entry.ID]; exists {
		return NewServerAlreadyExistsError(entry.ID.String())
	}

	m.servers[entry.ID] = server

	// Register with supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRegister,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: server, Config: cfg.Lifecycle},
	})

	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostRegister,
		Path:   entry.ID.String(),
		Data:   relay.Receiver(server),
	})

	return nil
}

// handleServerUpdate updates an existing HTTP server
func (m *Manager) handleServerUpdate(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.ServerConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	server, exists := m.servers[entry.ID]
	if !exists {
		return NewServerNotFoundError(entry.ID.String())
	}

	if err := server.UpdateConfig(cfg); err != nil {
		return NewUpdateConfigError(err)
	}
	m.pending[entry.ID] = true

	// Update supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceUpdate,
		Path:   entry.ID.String(),
		Data: &supervisor.Entry{
			Config: cfg.Lifecycle,
		},
	})

	return nil
}

// handleServerDelete removes an HTTP server
func (m *Manager) handleServerDelete(ctx context.Context, entry registry.Entry) error {
	_, exists := m.servers[entry.ID]
	if !exists {
		return NewServerNotFoundError(entry.ID.String())
	}

	// Done from supervisor
	m.bus.Send(ctx, event.Event{
		System: supervisor.System,
		Kind:   supervisor.ServiceRemove,
		Path:   entry.ID.String(),
	})

	// Done from process hosts
	m.bus.Send(ctx, event.Event{
		System: relay.System,
		Kind:   relay.HostDelete,
		Path:   entry.ID.String(),
	})

	// Clean up all mappings
	for routerID, serverID := range m.routerServers {
		if serverID.Equal(entry.ID) {
			delete(m.routerServers, routerID)
		}
	}

	for endpointID, routerID := range m.endpointRouters {
		if serverID, exists := m.routerServers[routerID]; exists && serverID.Equal(entry.ID) {
			delete(m.endpointRouters, endpointID)
		}
	}

	for staticID, serverID := range m.staticServers {
		if serverID.Equal(entry.ID) {
			delete(m.staticServers, staticID)
		}
	}

	delete(m.servers, entry.ID)
	delete(m.pending, entry.ID)
	return nil
}

//
// Router handlers
//

// handleRouterCreate adds a new router to a server
func (m *Manager) handleRouterCreate(_ context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.RouterConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	parsedServerID := registry.ParseID(cfg.Meta.GetString(config.ServerID, ""))
	serverID := parsedServerID.WithDefaultNS(entry.ID.NS)
	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	if err := server.UpsertRouter(entry.ID, cfg); err != nil {
		return err
	}

	m.log.Debug("added router",
		zap.String("router", entry.ID.String()),
		zap.String("prefix", cfg.Prefix),
		zap.String("server", serverID.String()),
		zap.Strings("middleware", cfg.Middleware),
		zap.Strings("post_middleware", cfg.PostMiddleware))

	m.routerServers[entry.ID] = serverID
	m.pending[serverID] = true
	return nil
}

// handleRouterUpdate updates an existing router
// CRITICAL: When moving a router between servers, we must ensure all endpoints
// are properly migrated to prevent data loss.
func (m *Manager) handleRouterUpdate(_ context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.RouterConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	// Get current server for this router
	currentServerID, exists := m.routerServers[entry.ID]
	if !exists {
		return NewRouterNotFoundError(entry.ID.String())
	}

	// Get target server from updated config
	parsedNewServerID := registry.ParseID(cfg.Meta.GetString(config.ServerID, ""))
	newServerID := parsedNewServerID.WithDefaultNS(entry.ID.NS)
	newServer, exists := m.servers[newServerID]
	if !exists {
		return NewServerNotFoundError(newServerID.String())
	}

	// If server changed, we need to handle endpoint migration
	if currentServerID != newServerID {
		// IMPORTANT: Currently we just delete from old and add to new
		// This means all endpoints attached to this router will be lost!
		// We should consider implementing proper endpoint migration in the future.

		// Done from old server
		if oldServer, exists := m.servers[currentServerID]; exists {
			if err := oldServer.DeleteRouter(entry.ID); err != nil {
				return err
			}
		}

		// Add to new server
		if err := newServer.UpsertRouter(entry.ID, cfg); err != nil {
			return err
		}

		// Update mapping
		m.routerServers[entry.ID] = newServerID
		m.pending[newServerID] = true

		m.log.Warn("router server changed - endpoints will need to be recreated",
			zap.String("router", entry.ID.String()),
			zap.String("old_server", currentServerID.String()),
			zap.String("new_server", newServerID.String()))
	} else {
		// Same server, just update the router
		if err := newServer.UpsertRouter(entry.ID, cfg); err != nil {
			return err
		}
		m.pending[currentServerID] = true
	}

	m.log.Debug("updated router",
		zap.String("router", entry.ID.String()),
		zap.String("prefix", cfg.Prefix),
		zap.String("server", newServerID.String()))

	return nil
}

// handleRouterDelete removes a router from a server
func (m *Manager) handleRouterDelete(_ context.Context, entry registry.Entry) error {
	serverID, exists := m.routerServers[entry.ID]
	if !exists {
		return NewRouterNotFoundError(entry.ID.String())
	}

	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	if err := server.DeleteRouter(entry.ID); err != nil {
		return err
	}

	m.log.Debug("deleted router",
		zap.String("router", entry.ID.String()),
		zap.String("server", serverID.String()))

	// Clean up endpoint mappings for this router
	for endpointID, routerID := range m.endpointRouters {
		if routerID.Equal(entry.ID) {
			delete(m.endpointRouters, endpointID)
		}
	}

	delete(m.routerServers, entry.ID)
	m.pending[serverID] = true
	return nil
}

//
// Endpoint handlers
//

// handleEndpointUpsert adds or updates an endpoint in a router
// Uses upsert pattern - if the endpoint already exists, it will be updated
func (m *Manager) handleEndpointUpsert(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.EndpointConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	parsedRouterID := registry.ParseID(cfg.Meta.GetString(config.RouterID, ""))
	routerID := parsedRouterID.WithDefaultNS(entry.ID.NS)
	serverID, exists := m.routerServers[routerID]
	if !exists {
		return NewRouterNotFoundError(routerID.String())
	}

	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	cfg.Func = cfg.Func.WithDefaultNS(entry.ID.NS)

	handler, err := m.endpointFactory.CreateHandler(ctx, cfg)
	if err != nil {
		return err
	}

	if err := server.UpsertEndpoint(routerID, entry.ID, cfg.Path, cfg.Method, handler); err != nil {
		return err
	}

	m.log.Debug("upserted endpoint",
		zap.String("endpoint", entry.ID.String()),
		zap.String("router", routerID.String()),
		zap.String("server", serverID.String()),
		zap.String("path", cfg.Path),
		zap.String("method", cfg.Method))

	// Track endpoint to router mapping
	m.endpointRouters[entry.ID] = routerID
	m.pending[serverID] = true
	return nil
}

// handleEndpointDelete removes an endpoint from a router
// Now works without requiring data by using the tracked endpoint-router mapping
func (m *Manager) handleEndpointDelete(_ context.Context, entry registry.Entry) error {
	routerID, exists := m.endpointRouters[entry.ID]
	if !exists {
		return NewEndpointNotFoundError(entry.ID.String())
	}

	serverID, exists := m.routerServers[routerID]
	if !exists {
		return NewRouterNotFoundError(routerID.String())
	}

	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	if err := server.RemoveEndpoint(routerID, entry.ID); err != nil {
		return err
	}

	m.log.Debug("deleted endpoint",
		zap.String("endpoint", entry.ID.String()),
		zap.String("router", routerID.String()),
		zap.String("server", serverID.String()))

	// Clean up the mapping
	delete(m.endpointRouters, entry.ID)
	m.pending[serverID] = true
	return nil
}

//
// Static handler handlers
//

// handleStaticUpsert adds or updates a static file handler
// Uses upsert pattern - if the handler already exists, it will be updated
func (m *Manager) handleStaticUpsert(ctx context.Context, entry registry.Entry) error {
	cfg, err := decodeEntity[config.StaticConfig](entry, m.dtt)
	if err != nil {
		return err
	}

	parsedServerID := registry.ParseID(cfg.Meta.GetString(config.ServerID, ""))
	serverID := parsedServerID.WithDefaultNS(entry.ID.NS)
	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	parsedFSID := registry.ParseID(cfg.FS.String())
	cfg.FS = parsedFSID.WithDefaultNS(entry.ID.NS)

	handler, err := m.staticFactory.CreateHandler(ctx, cfg)
	if err != nil {
		return err
	}

	if err := server.Mount(entry.ID, cfg.Path, handler); err != nil {
		return err
	}

	m.log.Debug("upserted static file handler",
		zap.String("static", entry.ID.String()),
		zap.String("server", serverID.String()),
		zap.String("path", cfg.Path))

	// Track static to server mapping
	m.staticServers[entry.ID] = serverID
	m.pending[serverID] = true
	return nil
}

// handleStaticDelete removes a static file handler
// Now works without requiring data by using the tracked static-server mapping
func (m *Manager) handleStaticDelete(_ context.Context, entry registry.Entry) error {
	serverID, exists := m.staticServers[entry.ID]
	if !exists {
		return NewStaticHandlerNotFoundError(entry.ID.String())
	}

	server, exists := m.servers[serverID]
	if !exists {
		return NewServerNotFoundError(serverID.String())
	}

	if err := server.Remove(entry.ID); err != nil {
		return err
	}

	m.log.Debug("deleted static file handler",
		zap.String("static", entry.ID.String()),
		zap.String("server", serverID.String()))

	// Clean up the mapping
	delete(m.staticServers, entry.ID)
	m.pending[serverID] = true
	return nil
}

//
// Helper functions
//

// decodeEntity is a helper to decode registry entries into specific configs
func decodeEntity[T any](entry registry.Entry, transcoder payload.Transcoder) (*T, error) {
	if entry.Data == nil {
		return nil, ErrConfigDataRequired
	}

	cfg := new(T)
	if err := transcoder.Unmarshal(entry.Data, cfg); err != nil {
		return nil, NewUnmarshalConfigError(err)
	}

	// set meta if applicable
	if metaHolder, ok := any(cfg).(interface{ SetMeta(attrs.Bag) }); ok {
		metaHolder.SetMeta(entry.Meta)
	}

	// Validate if the config implements Validate()
	if validator, ok := any(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, NewInvalidConfigError(err)
		}
	}

	return cfg, nil
}
