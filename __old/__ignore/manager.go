package __ignore

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/function"
	http2 "github.com/ponyruntime/pony/service/http"
	"net/http"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// ServerManager manages HTTP servers and their configurations
type ServerManager struct {
	log     *zap.Logger
	bus     events.Bus
	handler http.HandlerFunc
	dtt     payload.Transcoder

	servers         map[registry.ID]*http2.Server
	endpointServers map[registry.ID]registry.ID // endpoint Process -> server Process
	routerServers   map[registry.ID]registry.ID // router Process -> server Process
}

// NewManager creates a new HTTP service instance
func New2Manager(
	bus events.Bus, // todo: delete in favor of context
	dtt payload.Transcoder, // todo: delete in favor of context
	handler http.HandlerFunc,
	logger *zap.Logger,
) *ServerManager {
	return &ServerManager{
		log:             logger,
		bus:             bus,
		handler:         handler,
		dtt:             dtt,
		servers:         make(map[registry.ID]*http2.Server),
		endpointServers: make(map[registry.ID]registry.ID),
		routerServers:   make(map[registry.ID]registry.ID),
	}
}

// NewHTTPManager creates a new HTTP service instance with an executing runtime
func NewHTTPManager(
	bus events.Bus,
	dtt payload.Transcoder,
	exec function.Registry,
	logger *zap.Logger,
) *ServerManager {
	return &ServerManager{
		log:             logger,
		bus:             bus,
		handler:         NewEndpointHandler(exec, dtt, logger).Handle,
		dtt:             dtt,
		servers:         make(map[registry.ID]*http2.Server),
		endpointServers: make(map[registry.ID]registry.ID),
		routerServers:   make(map[registry.ID]registry.ID),
	}
}

// Add implements registry.EntryListener
func (s *ServerManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for create operation")
	}

	switch entry.Kind {
	case config.KindServer:
		return s.addServer(ctx, entry)
	case config.KindRouter:
		return s.addRouter(entry)
	case config.KindEndpoint:
		return s.addEndpoint(entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Update implements registry.EntryListener
func (s *ServerManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Data == nil {
		return fmt.Errorf("configuration data is required for update operation")
	}

	switch entry.Kind {
	case config.KindServer:
		return s.updateServer(ctx, entry)
	case config.KindRouter:
		return s.updateRouter(entry)
	case config.KindEndpoint:
		return s.updateEndpoint(entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}
}

// Delete implements registry.EntryListener
func (s *ServerManager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case config.KindServer:
		return s.deleteServer(ctx, entry)
	case config.KindRouter:
		return s.deleteRouter(entry)
	case config.KindEndpoint:
		return s.deleteEndpoint(entry)
	default:
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
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

func (s *ServerManager) addServer(ctx context.Context, entry registry.Entry) error {
	cfg := new(config.ServerConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	if _, exists := s.servers[entry.ID]; exists {
		return fmt.Errorf("server %s already exists", entry.ID)
	}

	server := NewServer(*cfg, s.handler)
	s.servers[entry.ID] = server

	s.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Register,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Service: server, Config: cfg.Lifecycle},
	})

	return nil
}

func (s *ServerManager) updateServer(ctx context.Context, entry registry.Entry) error {
	cfg := new(config.ServerConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	server, exists := s.servers[entry.ID]
	if !exists {
		return fmt.Errorf("server %s not found", entry.ID)
	}

	server.UpdateConfig(*cfg)

	s.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Update,
		Path:   entry.ID.String(),
		Data:   &supervisor.Entry{Config: cfg.Lifecycle},
	})

	return nil
}

func (s *ServerManager) deleteServer(ctx context.Context, entry registry.Entry) error {
	if _, exists := s.servers[entry.ID]; !exists {
		return fmt.Errorf("server %s not found", entry.ID)
	}

	s.bus.Send(ctx, events.Event{
		System: supervisor.System,
		Kind:   supervisor.Remove,
		Path:   entry.ID.String(),
	})

	// Clean up associated endpoints and routers
	for epID, srvID := range s.endpointServers {
		if srvID == entry.ID {
			delete(s.endpointServers, epID)
		}
	}

	for rID, srvID := range s.routerServers {
		if srvID == entry.ID {
			delete(s.routerServers, rID)
		}
	}

	delete(s.servers, entry.ID)
	return nil
}

func (s *ServerManager) addRouter(entry registry.Entry) error {
	cfg := new(config.RouterConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	serverIDStr := cfg.Meta.StringValue(config.ServerID)
	serverID := registry.ParseID(serverIDStr).WithDefaultNS(entry.ID.NS)
	server, exists := s.servers[serverID]
	if !exists {
		return fmt.Errorf("target server %s not found", serverID)
	}

	if err := server.router.AddRouter(entry.ID.String(), *cfg); err != nil {
		return fmt.Errorf("failed to add router: %w", err)
	}

	s.routerServers[entry.ID] = serverID
	return nil
}

func (s *ServerManager) updateRouter(entry registry.Entry) error {
	cfg := new(config.RouterConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	currentServerID, exists := s.routerServers[entry.ID]
	if !exists {
		return fmt.Errorf("router %s not found", entry.ID)
	}

	targetServerIDStr := cfg.Meta.StringValue(config.ServerID)
	targetServerID := registry.ParseID(targetServerIDStr).WithDefaultNS(entry.ID.NS)
	if _, exists := s.servers[targetServerID]; !exists {
		return fmt.Errorf("target server %s not found", targetServerID)
	}

	if currentServerID == targetServerID {
		return s.servers[currentServerID].router.UpdateRouter(entry.ID.String(), *cfg)
	}

	// Handle server migration
	if err := s.servers[targetServerID].router.AddRouter(entry.ID.String(), *cfg); err != nil {
		return err
	}

	s.routerServers[entry.ID] = targetServerID
	_ = s.servers[currentServerID].router.DeleteRouter(entry.ID.String())
	return nil
}

func (s *ServerManager) deleteRouter(entry registry.Entry) error {
	serverID, exists := s.routerServers[entry.ID]
	if !exists {
		return fmt.Errorf("router %s not found", entry.ID)
	}

	if err := s.servers[serverID].router.DeleteRouter(entry.ID.String()); err != nil {
		return fmt.Errorf("failed to delete router: %w", err)
	}

	delete(s.routerServers, entry.ID)
	return nil
}

func (s *ServerManager) addEndpoint(entry registry.Entry) error {
	cfg := new(config.EndpointConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	serverIDStr := cfg.Meta.StringValue(config.ServerID)
	serverID := registry.ParseID(serverIDStr).WithDefaultNS(entry.ID.NS)
	server, exists := s.servers[serverID]
	if !exists {
		return fmt.Errorf("target server %s not found", serverID)
	}

	if err := server.router.AddEndpoint(entry.ID.String(), *cfg); err != nil {
		return fmt.Errorf("failed to add endpoint: %w", err)
	}

	s.endpointServers[entry.ID] = serverID
	return nil
}

func (s *ServerManager) updateEndpoint(entry registry.Entry) error {
	cfg := new(config.EndpointConfig)
	if err := s.unmarshalAndValidate(entry.Data, cfg); err != nil {
		return err
	}

	currentServerID, exists := s.endpointServers[entry.ID]
	if !exists {
		return fmt.Errorf("endpoint %s not found", entry.ID)
	}

	targetServerIDStr := cfg.Meta.StringValue(config.ServerID)
	targetServerID := registry.ParseID(targetServerIDStr).WithDefaultNS(entry.ID.NS)
	if _, exists := s.servers[targetServerID]; !exists {
		return fmt.Errorf("target server %s not found", targetServerID)
	}

	if currentServerID == targetServerID {
		return s.servers[currentServerID].router.UpdateEndpoint(entry.ID.String(), *cfg)
	}

	// Handle server migration
	if err := s.servers[targetServerID].router.AddEndpoint(entry.ID.String(), *cfg); err != nil {
		return err
	}

	s.endpointServers[entry.ID] = targetServerID
	_ = s.servers[currentServerID].router.DeleteEndpoint(entry.ID.String())
	return nil
}

func (s *ServerManager) deleteEndpoint(entry registry.Entry) error {
	serverID, exists := s.endpointServers[entry.ID]
	if !exists {
		return fmt.Errorf("endpoint %s not found", entry.ID)
	}

	if err := s.servers[serverID].router.DeleteEndpoint(entry.ID.String()); err != nil {
		return fmt.Errorf("failed to delete endpoint: %w", err)
	}

	delete(s.endpointServers, entry.ID)
	return nil
}
