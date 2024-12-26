package http

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	httpapi "github.com/ponyruntime/pony/api/service/http"
	"log"
	"sync"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/pkg/eventbus"
	"go.uber.org/zap"
)

// Service manages multiple HTTP servers and their endpoints based on registry configuration
type Service struct {
	ctx        context.Context
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
			s.sendRejection(entry, err)
			return
		}

		log.Printf(">>>>>>>>>>>>service config!!: %+v", cfg)

		s.sendAcceptance(entry)

	case httpapi.KindEndpoint:
		s.log.Info("endpoint", zap.Any("event", evt))

		cfg := new(httpapi.EndpointConfig)
		err := s.dtt.Unmarshal(entry.Data, cfg)
		if err != nil {
			s.sendRejection(entry, err)
			return
		}

		s.sendAcceptance(entry)
	}
}

//	func (s *Lifecycle) handleServerEvent(kind events.Kind, entry registry.Entry) {
//		s.mu.Lock()
//		defer s.mu.Unlock()
//
//		switch kind {
//		case registry.Create:
//			if err := s.createServer(entry); err != nil {
//				s.log.Error("failed to create service",
//					zap.String("server_id", string(entry.ID)),
//					zap.Error(err),
//				)
//				s.sendRejection(entry)
//				return
//			}
//			s.sendAcceptance(entry)
//
//		case registry.Update:
//			if err := s.updateServer(entry); err != nil {
//				s.log.Error("failed to update service",
//					zap.String("server_id", string(entry.ID)),
//					zap.Error(err),
//				)
//				s.sendRejection(entry)
//				return
//			}
//			s.sendAcceptance(entry)
//
//		case registry.Delete:
//			if err := s.deleteServer(entry); err != nil {
//				s.log.Error("failed to delete service",
//					zap.String("server_id", string(entry.ID)),
//					zap.Error(err),
//				)
//				s.sendRejection(entry)
//				return
//			}
//			s.sendAcceptance(entry)
//		}
//	}
//
//	func (s *Lifecycle) handleEndpointEvent(kind events.Kind, entry registry.Entry) {
//		s.mu.Lock()
//		defer s.mu.Unlock()
//
//		// Extract service ID from endpoint ID (assuming format: "servers/[server_id]/endpoints/[endpoint_id]")
//		serverID := extractServerID(entry.ID)
//		service, exists := s.servers[serverID]
//		if !exists {
//			s.log.Error("service not found for endpoint",
//				zap.String("endpoint_id", string(entry.ID)),
//				zap.String("server_id", string(serverID)),
//			)
//			s.sendRejection(entry)
//			return
//		}
//
//		var config EndpointConfig
//		if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//			s.log.Error("failed to unmarshal endpoint config",
//				zap.String("endpoint_id", string(entry.ID)),
//				zap.Error(err),
//			)
//			s.sendRejection(entry)
//			return
//		}
//
//		switch kind {
//		case registry.Create, registry.Update:
//			if err := service.Router().AddEndpoint(string(entry.ID), config); err != nil {
//				s.log.Error("failed to add/update endpoint",
//					zap.String("endpoint_id", string(entry.ID)),
//					zap.Error(err),
//				)
//				s.sendRejection(entry)
//				return
//			}
//			s.sendAcceptance(entry)
//
//		case registry.Delete:
//			if err := service.Router().RemoveEndpoint(string(entry.ID)); err != nil {
//				s.log.Error("failed to remove endpoint",
//					zap.String("endpoint_id", string(entry.ID)),
//					zap.Error(err),
//				)
//				s.sendRejection(entry)
//				return
//			}
//			s.sendAcceptance(entry)
//		}
//	}
//
//	func (s *Lifecycle) createServer(entry registry.Entry) error {
//		var config ServerConfig
//		if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//			return fmt.Errorf("failed to unmarshal service config: %w", err)
//		}
//
//		// Create new service instance
//		service := NewServer(config, nil) // TODO: Add proper handler
//
//		// Register service with supervisor
//		s.bus.Send(s.ctx, events.Event{
//			System: supervisor.System,
//			Kind:   supervisor.Register,
//			Path:   events.Path(entry.ID),
//			Data: &supervisor.Entry{
//				Lifecycle: service,
//				Config:  config.Lifecycle,
//			},
//		})
//
//		s.servers[entry.ID] = service
//		return nil
//	}
//
//	func (s *Lifecycle) updateServer(entry registry.Entry) error {
//		service, exists := s.servers[entry.ID]
//		if !exists {
//			return fmt.Errorf("service not found: %s", entry.ID)
//		}
//
//		var config ServerConfig
//		if err := entry.Data.(payload.Payload).Unmarshal(&config); err != nil {
//			return fmt.Errorf("failed to unmarshal service config: %w", err)
//		}
//
//		service.UpdateConfig(config)
//		return nil
//	}
//
//	func (s *Lifecycle) deleteServer(entry registry.Entry) error {
//		if _, exists := s.servers[entry.ID]; !exists {
//			return fmt.Errorf("service not found: %s", entry.ID)
//		}
//
//		// Unregister from supervisor
//		s.bus.Send(s.ctx, events.Event{
//			System: supervisor.System,
//			Kind:   supervisor.Remove,
//			Path:   events.Path(entry.ID),
//		})

//		delete(s.servers, entry.ID)
//		return nil
//	}

func (s *Service) sendAcceptance(entry registry.Entry) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Accept,
		Path:   events.Path(entry.ID),
	})
}

func (s *Service) sendRejection(entry registry.Entry, err error) {
	s.bus.Send(s.ctx, events.Event{
		System: registry.System,
		Kind:   registry.Reject,
		Path:   events.Path(entry.ID),
		Data:   err,
	})
}
