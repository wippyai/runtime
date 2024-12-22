package http

//
//import (
//	"context"
//	"fmt"
//	"net/http"
//	"sync"
//	"time"
//
//	"github.com/ponyruntime/pony/api/events"
//	"github.com/ponyruntime/pony/api/payload"
//	"github.com/ponyruntime/pony/api/registry"
//	"github.com/ponyruntime/pony/pkg/entry"
//	"go.uber.org/zap"
//)
//
//// Supervisor manages Timeouts servers and their configurations
//type Supervisor struct {
//	log      *zap.Logger
//	bus      events.Bus
//	exec     Executor
//	ctx      context.Context
//	cancel   context.CancelFunc
//	wg       sync.WaitGroup
//	listener *entry.Listener
//
//	// Managed resources
//	servers map[string]*Server
//	mu      sync.RWMutex
//
//	// Main handler for all Timeouts requests
//	handler http.HandlerFunc
//}
//
//// NewSupervisor creates a new Timeouts supervisor
//func NewSupervisor(log *zap.Logger, bus events.Bus, exec Executor, transcoder payload.Transcoder, handler http.HandlerFunc) (*Supervisor, error) {
//	ctx, cancel := context.WithCancel(context.Background())
//
//	s := &Supervisor{
//		log:     log,
//		bus:     bus,
//		exec:    exec,
//		ctx:     ctx,
//		cancel:  cancel,
//		servers: make(map[string]*Server),
//		handler: handler,
//	}
//
//	// Create channel for configuration events
//	configCh := make(chan registry.Operation, 100)
//
//	// Set up configuration listener
//	listener, err := entry.NewListener(
//		ctx,
//		bus,
//		"http.*",
//		map[registry.Kind]func() interface{}{
//			"http.server":   func() interface{} { return &ServerConfig{} },
//			"http.router":   func() interface{} { return &RouterConfig{} },
//			"http.endpoint": func() interface{} { return &EndpointConfig{} },
//		},
//		configCh,
//		transcoder,
//	)
//	if err != nil {
//		cancel()
//		return nil, fmt.Errorf("failed to create config listener: %w", err)
//	}
//
//	s.listener = listener
//
//	// StartComponent processing configuration events
//	s.wg.Add(1)
//	go s.processConfigs(configCh)
//
//	return s, nil
//}
//
//// processConfigs handles incoming configuration events
//func (s *Supervisor) processConfigs(ch <-chan registry.Operation) {
//	defer s.wg.Done()
//
//	for {
//		select {
//		case <-s.ctx.Done():
//			return
//		case op, ok := <-ch:
//			if !ok {
//				return
//			}
//
//			s.log.Debug("Received configuration event",
//				zap.String("kind", string(op.Kind)),
//				zap.String("path", string(op.Entry.Name)))
//
//			var err error
//			switch op.Kind {
//			case "entry.create", "entry.update":
//				err = s.handleConfigUpdate(op)
//			case "entry.delete":
//				err = s.handleConfigDelete(op)
//			}
//
//			if err != nil {
//				s.log.Error("Failed to process config",
//					zap.String("kind", string(op.Kind)),
//					zap.String("path", string(op.Entry.Name)),
//					zap.Error(err))
//				s.listener.RejectLast(err)
//				continue
//			}
//
//			s.listener.AcceptLast()
//		}
//	}
//}
//
//// handleConfigUpdate processes configuration updates
//func (s *Supervisor) handleConfigUpdate(op registry.Operation) error {
//	switch cfg := op.Data.(type) {
//	case *ServerConfig:
//		return s.updateServer(op.Entry.Name, cfg)
//	case *RouterConfig:
//		return s.updateRouter(op.Entry.Name, cfg)
//	case *EndpointConfig:
//		return s.updateEndpoint(op.Entry.Name, cfg)
//	default:
//		return fmt.Errorf("unknown config type: %T", op.Data)
//	}
//}
//
//// handleConfigDelete processes configuration deletions
//func (s *Supervisor) handleConfigDelete(op registry.Operation) error {
//	switch op.Entry.Kind {
//	case "http.server":
//		return s.deleteServer(op.Entry.Name)
//	case "http.router":
//		return s.deleteRouter(op.Entry.Name)
//	case "http.endpoint":
//		return s.deleteEndpoint(op.Entry.Name)
//	default:
//		return fmt.Errorf("unknown config kind: %s", op.Entry.Kind)
//	}
//}
//
//// updateServer handles server configuration updates
//func (s *Supervisor) updateServer(path registry.Name, cfg *ServerConfig) error {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	serverID := cfg.Meta.StringValue("server_id")
//	if serverID == "" {
//		return fmt.Errorf("server_id is required in metadata")
//	}
//
//	tasks := []Task{
//		{
//			Name: fmt.Sprintf("Update server %s", serverID),
//			Execute: func(ctx context.Context) error {
//				server, exists := s.servers[serverID]
//				if !exists {
//					// Create new server
//					server = NewServer(*cfg, s.handler)
//					s.servers[serverID] = server
//
//					// StartComponent the server
//					go func() {
//						if err := server.Serve(s.ctx); err != nil {
//							s.log.Error("Server error",
//								zap.String("server_id", serverID),
//								zap.Error(err))
//						}
//					}()
//				} else {
//					// Update existing server
//					server.UpdateConfig(*cfg)
//				}
//				return nil
//			},
//			Rollback: func(ctx context.Context) error {
//				if oldServer, exists := s.servers[serverID]; exists {
//					oldServer.Stop(ctx)
//					delete(s.servers, serverID)
//				}
//				return nil
//			},
//		},
//	}
//
//	return s.exec.Execute(s.ctx, tasks)
//}
//
//// updateRouter handles router configuration updates
//func (s *Supervisor) updateRouter(path registry.Name, cfg *RouterConfig) error {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	serverID := cfg.Meta.StringValue("server_id")
//	if serverID == "" {
//		return fmt.Errorf("server_id is required in metadata")
//	}
//
//	server, exists := s.servers[serverID]
//	if !exists {
//		return fmt.Errorf("server %s not found", serverID)
//	}
//
//	tasks := []Task{
//		{
//			Name: fmt.Sprintf("Update router for server %s", serverID),
//			Execute: func(ctx context.Context) error {
//				return server.Router().AddRouter(*cfg)
//			},
//			Rollback: func(ctx context.Context) error {
//				routerID := cfg.Meta.StringValue("router_id")
//				return server.Router().DeleteRouter(routerID)
//			},
//		},
//	}
//
//	return s.exec.Execute(s.ctx, tasks)
//}
//
//// updateEndpoint handles endpoint configuration updates
//func (s *Supervisor) updateEndpoint(path registry.Name, cfg *EndpointConfig) error {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	serverID := cfg.Meta.StringValue("server_id")
//	if serverID == "" {
//		return fmt.Errorf("server_id is required in metadata")
//	}
//
//	server, exists := s.servers[serverID]
//	if !exists {
//		return fmt.Errorf("server %s not found", serverID)
//	}
//
//	endpointID := cfg.Meta.StringValue("endpoint_id")
//	if endpointID == "" {
//		endpointID = path.String()
//	}
//
//	tasks := []Task{
//		{
//			Name: fmt.Sprintf("Update endpoint %s", endpointID),
//			Execute: func(ctx context.Context) error {
//				return server.Router().AddEndpoint(endpointID, *cfg)
//			},
//			Rollback: func(ctx context.Context) error {
//				return server.Router().DeleteEndpoint(endpointID)
//			},
//		},
//	}
//
//	return s.exec.Execute(s.ctx, tasks)
//}
//
//// deleteServer handles server deletion
//func (s *Supervisor) deleteServer(path registry.Name) error {
//	s.mu.Lock()
//	defer s.mu.Unlock()
//
//	serverID := path.String()
//	server, exists := s.servers[serverID]
//	if !exists {
//		return nil
//	}
//
//	tasks := []Task{
//		{
//			Name: fmt.Sprintf("Delete server %s", serverID),
//			Execute: func(ctx context.Context) error {
//				if err := server.Stop(ctx); err != nil {
//					return fmt.Errorf("failed to stop server: %w", err)
//				}
//				delete(s.servers, serverID)
//				return nil
//			},
//		},
//	}
//
//	return s.exec.Execute(s.ctx, tasks)
//}
//
//// deleteRouter handles router deletion
//func (s *Supervisor) deleteRouter(path registry.Name) error {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	routerPath := path.String()
//	routerID := routerPath // Extract router Name from path or metadata
//
//	// Find the server that owns this router
//	for _, server := range s.servers {
//		if err := server.Router().DeleteRouter(routerID); err == nil {
//			return nil
//		}
//	}
//
//	return fmt.Errorf("router %s not found in any server", routerID)
//}
//
//// deleteEndpoint handles endpoint deletion
//func (s *Supervisor) deleteEndpoint(path registry.Name) error {
//	s.mu.RLock()
//	defer s.mu.RUnlock()
//
//	endpointPath := path.String()
//	endpointID := endpointPath // Extract endpoint Name from path or metadata
//
//	// Find the server that owns this endpoint
//	for _, server := range s.servers {
//		if err := server.Router().DeleteEndpoint(endpointID); err == nil {
//			return nil
//		}
//	}
//
//	return fmt.Errorf("endpoint %s not found in any server", endpointID)
//}
//
//// Close stops the supervisor and all managed servers
//func (s *Supervisor) Close() error {
//	s.cancel()
//	s.listener.Close()
//
//	// Create context with timeout for shutdown
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	// Stop all servers
//	s.mu.Lock()
//	for id, server := range s.servers {
//		s.log.Info("Stopping server", zap.String("server_id", id))
//		if err := server.Stop(ctx); err != nil {
//			s.log.Error("Failed to stop server",
//				zap.String("server_id", id),
//				zap.Error(err))
//		}
//	}
//	s.mu.Unlock()
//
//	s.wg.Wait()
//	return nil
//}
