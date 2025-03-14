package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	// BootTimeout is the maximum time to wait for the server to start
	BootTimeout = 30 * time.Second

	// CheckInterval is the interval between server availability checks during startup
	CheckInterval = 100 * time.Millisecond

	// StatusBuffer is the size of the status channel buffer
	StatusBuffer = 10
)

// ContextListener is the context key for the HTTP listener
var ContextListener = &contextapi.Key{Name: "listener"}

// ServerService combines HTTP server and router functionality
type ServerService struct {
	ctx           context.Context
	id            registry.ID
	config        *config.ServerConfig
	routeMgr      *RouteManager
	server        *http.Server
	mu            sync.RWMutex
	statusChan    chan any
	started       bool                   // Track if server has been started
	mountPaths    map[registry.ID]string // Track mount paths by Source
	host          pubsub.Host            // pubsub host
	middlewareFac MiddlewareAPI          // Middleware factory
}

// NewServerService creates a new ServerService instance
func NewServerService(id registry.ID, cfg *config.ServerConfig, middleware MiddlewareAPI) *ServerService {
	return &ServerService{
		id:            id,
		config:        cfg,
		routeMgr:      NewRouteManager(),
		statusChan:    make(chan any, StatusBuffer),
		mountPaths:    make(map[registry.ID]string),
		middlewareFac: middleware,
	}
}

// UpdateConfig updates the server configuration
// Returns an error if trying to change the address while the server is running
func (s *ServerService) UpdateConfig(cfg *config.ServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if address changes while server is running
	if s.started {
		if s.config.Addr != cfg.Addr {
			return fmt.Errorf("cannot change server address while running")
		}
	}

	// Always update the config
	s.config = cfg
	return nil
}

// UpsertRouter adds a new router or updates an existing one with the provided configuration
func (s *ServerService) UpsertRouter(id registry.ID, cfg *config.RouterConfig) error {
	// Convert middleware config to actual middleware functions
	middlewares := make([]func(http.Handler) http.Handler, 0, len(cfg.Middleware))
	if s.middlewareFac != nil {
		for _, mw := range cfg.Middleware {
			if fn := s.middlewareFac.CreateMiddleware(mw, cfg.Options); fn != nil {
				middlewares = append(middlewares, fn)
			}
		}
	}

	return s.routeMgr.AddRouter(id, cfg.Prefix, middlewares)
}

// DeleteRouter removes a router by Source
func (s *ServerService) DeleteRouter(id registry.ID) error {
	return s.routeMgr.RemoveRouter(id)
}

// UpsertEndpoint adds or updates an endpoint in the specified router
func (s *ServerService) UpsertEndpoint(routerID, id registry.ID, path string, method string, handler http.Handler) error {
	return s.routeMgr.AddRoute(routerID, id, method, path, id, handler)
}

// RemoveEndpoint removes an endpoint from the specified router
func (s *ServerService) RemoveEndpoint(routerID, id registry.ID) error {
	return s.routeMgr.RemoveRoute(routerID, id)
}

// Mount adds a handler at the specified path and tracks it by Source
func (s *ServerService) Mount(id registry.ID, path string, handler http.Handler) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.routeMgr.Mount(path, handler); err != nil {
		return err
	}

	// Store path mapping for later unmount
	s.mountPaths[id] = path
	return nil
}

// Remove unmounts a handler by Source
func (s *ServerService) Remove(id registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, exists := s.mountPaths[id]
	if !exists {
		return fmt.Errorf("mount for Source %s not found", id)
	}

	if err := s.routeMgr.Unmount(path); err != nil {
		return err
	}

	// Clean up the mapping
	delete(s.mountPaths, id)
	return nil
}

// Rebuild rebuilds the entire router with the current configuration
func (s *ServerService) Rebuild(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.routeMgr.Build()

	// If server is running, we need to update its handler
	if s.started && s.server != nil {
		s.server.Handler = s.routeMgr
	}

	return nil
}

// Start implements the supervisor.Service interface to start the HTTP server
func (s *ServerService) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()

	// Initialize host with config
	hostConfig := pubsubsys.HostConfig{
		BufferSize:  s.config.Host.BufferSize,
		WorkerCount: s.config.Host.WorkerCount,
		Logger:      logs.GetLogger(ctx),
	}

	// If values not specified, set reasonable defaults
	if hostConfig.BufferSize <= 0 {
		hostConfig.BufferSize = 1024 // Default buffer size
	}

	if hostConfig.WorkerCount <= 0 {
		hostConfig.WorkerCount = runtime.NumCPU() // Default to number of CPUs
	}

	// Create the host
	s.host = pubsubsys.NewHost(ctx, hostConfig)

	ctx = context.WithValue(ctx, config.ContextServerID, s.id)
	ctx = pubsub.WithHost(ctx, s)
	s.ctx = ctx

	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.routeMgr,
		ReadTimeout:  s.config.Timeouts.ReadTimeout,
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext: func(l net.Listener) context.Context {
			return context.WithValue(s.ctx, ContextListener, l)
		},
	}
	s.started = true

	s.mu.Unlock()

	// Launch server
	go func() {
		err := s.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case s.statusChan <- fmt.Errorf("server error: %w", err):
			default:
			}
		}

		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
	}()

	if err := s.ensureRunning(ctx); err != nil {
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
		return nil, fmt.Errorf("startup check failed: %w", err)
	}

	// Handle shutdown via context
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := s.Stop(shutdownCtx); err != nil {
			select {
			case s.statusChan <- fmt.Errorf("shutdown error: %w", err):
			default:
			}
		}
	}()

	select {
	case s.statusChan <- fmt.Sprintf("service listening on %s", s.config.Addr):
	default:
	}

	return s.statusChan, nil
}

// Stop implements the supervisor.Service interface to stop the HTTP server
func (s *ServerService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Gracefully shutdown the server
	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.server = nil
	}

	// Host will be cleaned up via context cancellation
	s.host = nil
	s.started = false

	return nil
}

// ensureRunning verifies if the server is listening on its configured address
func (s *ServerService) ensureRunning(ctx context.Context) error {
	timeout := time.After(BootTimeout)
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("service failed to start within %v timeout", BootTimeout)
		case <-ctx.Done():
			return fmt.Errorf("startup canceled: %w", ctx.Err())
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", s.config.Addr, time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

// createMiddleware converts a middleware name to its handler function
// This is now a fallback if no middleware factory is provided
func (s *ServerService) createMiddleware(name string, options map[string]string) func(http.Handler) http.Handler {
	switch name {
	case "timeout":
		timeoutVal := options["timeout"]
		if timeoutVal == "" {
			timeoutVal = "60s"
		}
		duration, err := time.ParseDuration(timeoutVal)
		if err == nil {
			return middleware.Timeout(duration)
		}
	case "recoverer":
		return middleware.Recoverer
	case "request_id":
		return middleware.RequestID
	case "real_ip":
		return middleware.RealIP
	}

	return nil
}

// Implement Host interface methods by delegating to embedded host

// Attach implements pubsub.Host Attach method
func (s *ServerService) Attach(pid pubsub.PID, ch chan *pubsub.Package) (context.CancelFunc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return nil, fmt.Errorf("server host not initialized")
	}

	return s.host.Attach(pid, ch)
}

// Detach implements pubsub.Host Detach method
func (s *ServerService) Detach(pid pubsub.PID) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return
	}

	s.host.Detach(pid)
}

// Send implements pubsub.Host Send method
func (s *ServerService) Send(pkg *pubsub.Package) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return fmt.Errorf("server host not initialized")
	}

	return s.host.Send(pkg)
}

// Ensure ServerService implements required interfaces
var (
	_ supervisor.Service = (*ServerService)(nil)
	_ Server             = (*ServerService)(nil)
)
