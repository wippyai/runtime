package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"net/http"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// Service manages multiple HTTP servers and their endpoints based on registry configuration
type Service struct {
	ctx     context.Context
	log     *zap.Logger
	bus     events.Bus
	dtt     payload.Transcoder
	scr     *eventbus.Subscriber
	mu      sync.RWMutex
	servers map[registry.ID]*Server
}

// Init creates a new HTTP service instance
func Init(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Service {
	return &Service{
		log:     logger,
		bus:     bus,
		dtt:     dtt,
		servers: make(map[registry.ID]*Server),
	}
}

// Start begins listening for registry events
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctx = ctx
	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		registry.System,
		registry.Changes,
		s.handleEvent,
	)

	if err != nil {
		return fmt.Errorf("failed to create scr: %w", err)
	}
	s.scr = sub
	return nil
}

// Stop gracefully shuts down all servers and stops listening for events
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scr != nil {
		s.scr.Close()
	}

	s.servers = make(map[registry.ID]*Server) // lifecycle delegated to supervisor
	s.scr = nil

	return nil
}

func (s *Service) handleEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		s.log.Error("invalid registry event data", zap.Any("event", evt))
		return
	}

	switch entry.Kind {
	case httpapi.KindServer:
		cfg := new(httpapi.ServerConfig)
		err := s.dtt.Unmarshal(entry.Data, cfg)
		if err != nil {
			s.reject(entry.ID, err)
			return
		}

		if err := cfg.Validate(); err != nil {
			s.reject(entry.ID, fmt.Errorf("invalid configuration: %w", err))
			return
		}

		s.handleServer(entry.ID, evt.Kind, *cfg)

	case httpapi.KindRouter:
		cfg := new(httpapi.RouterConfig)
		err := s.dtt.Unmarshal(entry.Data, cfg)
		if err != nil {
			s.reject(entry.ID, err)
			return
		}

		if err := cfg.Validate(); err != nil {
			s.reject(entry.ID, fmt.Errorf("invalid configuration: %w", err))
			return
		}

		s.handleRouter(entry.ID, evt.Kind, *cfg)

	case httpapi.KindEndpoint:
		cfg := new(httpapi.EndpointConfig)
		err := s.dtt.Unmarshal(entry.Data, cfg)
		if err != nil {
			s.reject(entry.ID, err)
			return
		}

		if err := cfg.Validate(); err != nil {
			s.reject(entry.ID, fmt.Errorf("invalid configuration: %w", err))
			return
		}

		s.handleEndpoint(entry.ID, evt.Kind, *cfg)
	}
}

func (s *Service) handleServer(id registry.ID, kind events.Kind, cfg httpapi.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch kind {
	case registry.Create:
		if _, exists := s.servers[id]; exists {
			s.reject(id, fmt.Errorf("server %s already exists", id))
			return
		}

		// Create new server instance
		server := NewServer(cfg, func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte("Hello, World!"))
		})

		s.servers[id] = server

		// Register with supervisor
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

		// Update server config
		server.UpdateConfig(cfg)

		// Update supervisor lifecycle config
		s.bus.Send(s.ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Update,
			Path:   events.Path(id),
			Data:   &supervisor.Entry{Config: cfg.Lifecycle}, // only lifecycle config can be updated
		})

	case registry.Delete:
		if _, exists := s.servers[id]; !exists {
			s.reject(id, fmt.Errorf("server %s not found", id))
			return
		}

		// Unregister from supervisor
		s.bus.Send(s.ctx, events.Event{
			System: supervisor.System,
			Kind:   supervisor.Remove,
			Path:   events.Path(id),
		})

		// Remove from our local registry
		delete(s.servers, id)
	}

	s.accept(id)
}

func (s *Service) handleRouter(id registry.ID, kind events.Kind, cfg httpapi.RouterConfig) {
	s.reject(id, fmt.Errorf("not implemented"))
}

func (s *Service) handleEndpoint(id registry.ID, kind events.Kind, cfg httpapi.EndpointConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	serverID := cfg.Meta.StringValue(httpapi.ServerID)
	server, exists := s.servers[registry.ID(serverID)]
	if !exists {
		s.reject(id, fmt.Errorf("http server %s not found", serverID))
		return
	}

	switch kind {
	case registry.Create:
		if err := server.Router().AddEndpoint(string(id), cfg); err != nil {
			s.log.Error("failed to add endpoint",
				zap.String("endpoint_id", string(id)),
				zap.Error(err),
			)
			s.reject(id, err)
			return
		}

	case registry.Update:
		if err := server.Router().UpdateEndpoint(string(id), cfg); err != nil {
			s.log.Error("failed to update endpoint",
				zap.String("endpoint_id", string(id)),
				zap.Error(err),
			)
			s.reject(id, err)
			return
		}

	case registry.Delete:
		if err := server.Router().DeleteEndpoint(string(id)); err != nil {
			s.log.Error("failed to delete endpoint",
				zap.String("endpoint_id", string(id)),
				zap.Error(err),
			)
			s.reject(id, err)
			return
		}
	}

	s.accept(id)
}

func (s *Service) accept(id registry.ID) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   events.Path(id),
	})
}

func (s *Service) reject(id registry.ID, err error) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   events.Path(id),
		Data:   err,
	})
}
