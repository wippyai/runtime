package http

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// ServerManager manages multiple HTTP servers and their endpoints based on registry configuration
type ServerManager struct {
	ctx     context.Context
	log     *zap.Logger
	bus     events.Bus
	handler http.HandlerFunc
	dtt     payload.Transcoder
	scr     *eventbus.Subscriber
	mu      sync.RWMutex

	// Core server registry
	servers map[registry.ID]*Server

	// Mappings to track relationships
	endpointServers map[registry.ID]registry.ID // endpoint ID -> server ID
	routerServers   map[registry.ID]registry.ID // router ID -> server ID
}

// Init creates a new HTTP service instance
func Init(
	bus events.Bus,
	dtt payload.Transcoder,
	handler http.HandlerFunc,
	logger *zap.Logger,
) *ServerManager {
	return &ServerManager{
		log:             logger,
		bus:             bus,
		handler:         handler,
		dtt:             dtt,
		servers:         make(map[registry.ID]*Server),
		endpointServers: make(map[registry.ID]registry.ID),
		routerServers:   make(map[registry.ID]registry.ID),
	}
}

// Start begins listening for registry events
func (s *ServerManager) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx != nil {
		s.log.Error("server manager already started")
		return fmt.Errorf("server manager already started")
	}

	s.ctx = ctx
	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		registry.System,
		registry.Changes,
		s.handleEvent,
	)

	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.scr = sub
	return nil
}

// Stop gracefully shuts down all servers and stops listening for events
func (s *ServerManager) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scr != nil {
		s.scr.Close()
	}

	s.servers = make(map[registry.ID]*Server)
	s.endpointServers = make(map[registry.ID]registry.ID)
	s.routerServers = make(map[registry.ID]registry.ID)
	s.scr = nil

	return nil
}

func (s *ServerManager) handleEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		s.log.Error("invalid registry event data", zap.Any("event", evt))
		return
	}

	s.log.Debug("processing registry event",
		zap.String("id", string(entry.ID)),
		zap.String("kind", string(evt.Kind)),
		zap.String("type", string(entry.Kind)))

	// For create/update operations, ensure we have valid data
	if evt.Kind != registry.Delete && entry.Data == nil {
		s.reject(entry.ID, fmt.Errorf("configuration data is required for create/update operations"))
		return
	}

	switch entry.Kind {
	case config.KindServer:
		cfg := new(config.ServerConfig)
		if entry.Data != nil {
			if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
				s.reject(entry.ID, err)
				return
			}
		}
		s.handleServer(entry.ID, evt.Kind, cfg)

	case config.KindRouter:
		cfg := new(config.RouterConfig)
		if entry.Data != nil {
			if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
				s.reject(entry.ID, err)
				return
			}
		}
		s.handleRouter(entry.ID, evt.Kind, cfg)

	case config.KindEndpoint:
		cfg := new(config.EndpointConfig)
		if entry.Data != nil {
			if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
				s.reject(entry.ID, err)
				return
			}
		}
		s.handleEndpoint(entry.ID, evt.Kind, cfg)
	}
}

func (s *ServerManager) unmarshalAndValidate(data payload.Payload, cfg interface{}) error {
	if err := s.dtt.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if validator, ok := cfg.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}
	}

	return nil
}

func (s *ServerManager) handleServer(id registry.ID, kind events.Kind, cfg *config.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case registry.Create:
		if _, exists := s.servers[id]; exists {
			s.reject(id, fmt.Errorf("server %s already exists", id))
			return
		}

		server := NewServer(*cfg, s.handler)
		s.servers[id] = server

		// launch the server (if auto-start is enabled)
		s.bus.Send(s.ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Register,
			Path:   events.Path(id),
			Data:   &supervisor.Entry{Service: server, Config: cfg.Lifecycle},
		})

	case registry.Update:
		server, exists := s.servers[id]
		if !exists {
			s.reject(id, fmt.Errorf("server %s not found", id))
			return
		}

		server.UpdateConfig(*cfg)
		s.bus.Send(s.ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Update,
			Path:   events.Path(id),
			Data:   &supervisor.Entry{Config: cfg.Lifecycle},
		})

	case registry.Delete:
		if _, exists := s.servers[id]; !exists {
			s.reject(id, fmt.Errorf("server %s not found", id))
			return
		}

		// Clean up all associated endpoints and routers
		for epID, srvID := range s.endpointServers {
			if srvID == id {
				delete(s.endpointServers, epID)
			}
		}

		for rID, srvID := range s.routerServers {
			if srvID == id {
				delete(s.routerServers, rID)
			}
		}

		s.bus.Send(s.ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(id),
		})

		delete(s.servers, id)
	}

	s.accept(id)
}
func (s *ServerManager) handleRouter(id registry.ID, kind events.Kind, cfg *config.RouterConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentServerID, exists := s.routerServers[id]
	targetServerID := registry.ID(cfg.Meta.StringValue(config.ServerID))

	switch kind {
	case registry.Create:
		// Check if the target server exists
		if _, exists := s.servers[targetServerID]; !exists {
			s.reject(id, fmt.Errorf("target server %s not found", targetServerID))
			return
		}
		// Add to the new server
		newServer := s.servers[targetServerID]
		if err := newServer.router.AddRouter(string(id), *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to add router to new server: %w", err))
			return
		}
		s.routerServers[id] = targetServerID

	case registry.Update:
		// Check if router exists
		if !exists {
			s.reject(id, fmt.Errorf("router %s not found in registry", id))
			return
		}
		// Check if the target server exists
		if _, exists := s.servers[targetServerID]; !exists {
			s.reject(id, fmt.Errorf("target server %s not found", targetServerID))
			return
		}

		if currentServerID == targetServerID {
			// Regular update
			server := s.servers[currentServerID]
			if err := server.router.UpdateRouter(string(id), *cfg); err != nil {
				s.reject(id, fmt.Errorf("failed to update router: %w", err))
				return
			}
		} else {
			// Migration:
			// 1. Add to the new server first
			newServer := s.servers[targetServerID]
			if err := newServer.router.AddRouter(string(id), *cfg); err != nil {
				s.reject(id, fmt.Errorf("failed to add router to new server: %w", err))
				return
			}

			// 2. Update mapping
			s.routerServers[id] = targetServerID

			// 3. Delete from the old server
			oldServer := s.servers[currentServerID]
			if err := oldServer.router.DeleteRouter(string(id)); err != nil {
				// never happens
				s.log.Error("failed to delete router from old server",
					zap.String("router_id", string(id)),
					zap.String("old_server_id", string(currentServerID)),
					zap.Error(err),
				)
			}
		}
	case registry.Delete:
		// Check if router exists
		if !exists {
			s.reject(id, fmt.Errorf("router %s not found in registry", id))
			return
		}
		server := s.servers[currentServerID]
		if err := server.router.DeleteRouter(string(id)); err != nil {
			s.reject(id, fmt.Errorf("failed to delete router: %w", err))
			return
		}
		delete(s.routerServers, id)
	}

	s.accept(id)
}

func (s *ServerManager) handleEndpoint(id registry.ID, kind events.Kind, cfg *config.EndpointConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	currentServerID, exists := s.endpointServers[id]
	targetServerID := registry.ID(cfg.Meta.StringValue(config.ServerID))

	switch kind {
	case registry.Create:
		// Check if the target server exists
		if _, exists := s.servers[targetServerID]; !exists {
			s.reject(id, fmt.Errorf("target server %s not found", targetServerID))
			return
		}
		// Add to the new server
		newServer := s.servers[targetServerID]
		if err := newServer.router.AddEndpoint(string(id), *cfg); err != nil {
			s.reject(id, fmt.Errorf("failed to add endpoint to new server: %w", err))
			return
		}
		s.endpointServers[id] = targetServerID

	case registry.Update:
		// Check if endpoint exists
		if !exists {
			s.reject(id, fmt.Errorf("endpoint %s not found in registry", id))
			return
		}

		// Check if the target server exists
		if _, exists := s.servers[targetServerID]; !exists {
			s.reject(id, fmt.Errorf("target server %s not found", targetServerID))
			return
		}

		if currentServerID == targetServerID {
			// Regular update
			server := s.servers[currentServerID]
			if err := server.router.UpdateEndpoint(string(id), *cfg); err != nil {
				s.reject(id, fmt.Errorf("failed to update endpoint: %w", err))
				return
			}
		} else {
			// Migration:
			// 1. Add to the new server first
			newServer := s.servers[targetServerID]
			if err := newServer.router.AddEndpoint(string(id), *cfg); err != nil {
				s.reject(id, fmt.Errorf("failed to add endpoint to new server: %w", err))
				return
			}

			// 2. Update mapping
			s.endpointServers[id] = targetServerID

			// 3. Delete from the old server
			oldServer := s.servers[currentServerID]
			if err := oldServer.router.DeleteEndpoint(string(id)); err != nil {
				// never happens
				s.log.Error("failed to delete endpoint from old server",
					zap.String("endpoint_id", string(id)),
					zap.String("old_server_id", string(currentServerID)),
					zap.Error(err),
				)
			}
		}

	case registry.Delete:
		// Check if endpoint exists
		if !exists {
			s.reject(id, fmt.Errorf("endpoint %s not found in registry", id))
			return
		}

		server := s.servers[currentServerID]
		if err := server.router.DeleteEndpoint(string(id)); err != nil {
			s.reject(id, fmt.Errorf("failed to delete endpoint: %w", err))
			return
		}
		delete(s.endpointServers, id)

	}

	s.accept(id)
}

func (s *ServerManager) accept(id registry.ID) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   events.Path(id),
	})
}

func (s *ServerManager) reject(id registry.ID, err error) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   events.Path(id),
		Data:   err,
	})
}
