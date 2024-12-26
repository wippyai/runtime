package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	httpapi "github.com/ponyruntime/pony/api/server/http"
	"log"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

const (
	kindServer   registry.Kind = "server"
	kindEndpoint registry.Kind = "endpoint"
)

// Service manages multiple HTTP servers and their endpoints based on registry configuration
type Service struct {
	ctx        context.Context
	cancel     context.CancelFunc
	log        *zap.Logger
	bus        events.Bus
	dtt        payload.Transcoder
	subscriber *eventbus.Subscriber
	mu         sync.RWMutex
	servers    map[registry.ID]*Server
}

// Init creates a new HTTP service instance
func Init(bus events.Bus, dtt payload.Transcoder, logger *zap.Logger) *Service {
	return &Service{
		log:     logger,
		bus:     bus,
		servers: make(map[registry.ID]*Server),
		dtt:     dtt,
	}
}

// Start begins listening for registry events
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub, err := eventbus.NewSubscriber(
		ctx,
		s.bus,
		registry.System,
		"entry.*",
		s.processEvent,
	)

	if err != nil {
		return fmt.Errorf("failed to create subscriber: %w", err)
	}
	s.subscriber = sub
	return nil
}

// Stop gracefully shuts down all servers and stops listening for events
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subscriber != nil {
		s.subscriber.Close()
	}

	s.servers = make(map[registry.ID]*Server) // lifecycle delegated to supervisor
	s.subscriber = nil

	return nil
}

func (s *Service) processEvent(evt events.Event) {
	entry, ok := evt.Data.(registry.Entry)
	if !ok {
		s.log.Error("invalid registry event data", zap.Any("event", evt))
		return
	}

	log.Printf("entry: %+v", entry)

	switch entry.Kind {
	case kindServer:
		s.log.Info("server event", zap.Any("event", evt))
		var cfg httpapi.ServerConfig
		err := s.dtt.Unmarshal(entry.Data, &cfg)
		if err != nil {
			s.log.Error("failed to unmarshal server config",
				zap.String("server_id", string(entry.ID)),
				zap.Error(err),
			)
			return
		}

		log.Printf("server config: %v", cfg)

		//		s.handleServerEvent(evt.Kind, entry)
		//	case kindEndpoint:
		//	s.handleEndpointEvent(evt.Kind, entry)
	}
}

//func (s *Service) handleServerEvent(kind events.Kind, entry registry.Entry) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	switch kind {
//	case registry.Create:
//		if err := s.createServer(entry); err != nil {
//			s.log.Error("failed to create server",
//				zap.String("server_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//		s.sendAcceptance(entry)
//
//	case registry.Update:
//		if err := s.updateServer(entry); err != nil {
//			s.log.Error("failed to update server",
//				zap.String("server_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//		s.sendAcceptance(entry)
//
//	case registry.Delete:
//		if err := s.deleteServer(entry); err != nil {
//			s.log.Error("failed to delete server",
//				zap.String("server_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//		s.sendAcceptance(entry)
//	}
//}
//
//func (s *Service) handleEndpointEvent(kind events.Kind, entry registry.Entry) {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	// Extract server ID from endpoint ID (assuming format: "servers/[server_id]/endpoints/[endpoint_id]")
//	serverID := extractServerID(entry.ID)
//	server, exists := s.servers[serverID]
//	if !exists {
//		s.log.Error("server not found for endpoint",
//			zap.String("endpoint_id", string(entry.ID)),
//			zap.String("server_id", string(serverID)),
//		)
//		s.sendRejection(entry)
//		return
//	}
//
//	var config EndpointConfig
//	if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//		s.log.Error("failed to unmarshal endpoint config",
//			zap.String("endpoint_id", string(entry.ID)),
//			zap.Error(err),
//		)
//		s.sendRejection(entry)
//		return
//	}
//
//	switch kind {
//	case registry.Create, registry.Update:
//		if err := server.Router().AddEndpoint(string(entry.ID), config); err != nil {
//			s.log.Error("failed to add/update endpoint",
//				zap.String("endpoint_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//		s.sendAcceptance(entry)
//
//	case registry.Delete:
//		if err := server.Router().RemoveEndpoint(string(entry.ID)); err != nil {
//			s.log.Error("failed to remove endpoint",
//				zap.String("endpoint_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//		s.sendAcceptance(entry)
//	}
//}
//
//func (s *Service) createServer(entry registry.Entry) error {
//	var config ServerConfig
//	if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//		return fmt.Errorf("failed to unmarshal server config: %w", err)
//	}
//
//	// Create new server instance
//	server := NewServer(config, nil) // TODO: Add proper handler
//
//	// Register server with supervisor
//	s.bus.Send(s.ctx, events.Event{
//		System: supervisor.System,
//		Kind:   supervisor.Register,
//		Path:   events.Path(entry.ID),
//		Data: &supervisor.Entry{
//			Service: server,
//			Config:  config.Service,
//		},
//	})
//
//	s.servers[entry.ID] = server
//	return nil
//}
//
//func (s *Service) updateServer(entry registry.Entry) error {
//	server, exists := s.servers[entry.ID]
//	if !exists {
//		return fmt.Errorf("server not found: %s", entry.ID)
//	}
//
//	var config ServerConfig
//	if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//		return fmt.Errorf("failed to unmarshal server config: %w", err)
//	}
//
//	server.UpdateConfig(config)
//	return nil
//}
//
//func (s *Service) deleteServer(entry registry.Entry) error {
//	if _, exists := s.servers[entry.ID]; !exists {
//		return fmt.Errorf("server not found: %s", entry.ID)
//	}
//
//	// Unregister from supervisor
//	s.bus.Send(s.ctx, events.Event{
//		System: supervisor.System,
//		Kind:   supervisor.Remove,
//		Path:   events.Path(entry.ID),
//	})
//
//	delete(s.servers, entry.ID)
//	return nil
//}
//
//func (s *Service) sendAcceptance(entry registry.Entry) {
//	s.bus.Send(s.ctx, events.Event{
//		System: registry.System,
//		Kind:   registry.Accept,
//		Data:   entry,
//	})
//}
//
//func (s *Service) sendRejection(entry registry.Entry) {
//	s.bus.Send(s.ctx, events.Event{
//		System: registry.System,
//		Kind:   registry.Reject,
//		Data:   entry,
//	})
//}
//
//// Helper function to extract server ID from endpoint ID
//func extractServerID(endpointID registry.ID) registry.ID {
//	// TODO: Implement proper path parsing
//	// Assuming format: "servers/[server_id]/endpoints/[endpoint_id]"
//	return endpointID
//}
