package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"

	relaysys "github.com/wippyai/runtime/system/relay"

	contextapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	config "github.com/wippyai/runtime/api/service/http"
	"github.com/wippyai/runtime/api/supervisor"
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
var ContextListener = &contextapi.Key{Name: "http.listener"}

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
	host          relay.AttachableHost   // pubsub host
	middlewareFac MiddlewareAPI          // Middleware factory
	handlerFunc   http.Handler           // Optional server-level handler
}

// NewServerService creates a new ServerService instance
func NewServerService(id registry.ID, cfg *config.ServerConfig, middleware MiddlewareAPI) (*ServerService, error) {
	routeManager, err := NewRouteManager()
	if err != nil {
		return nil, err
	}

	return &ServerService{
		id:            id,
		config:        cfg,
		routeMgr:      routeManager,
		statusChan:    make(chan any, StatusBuffer),
		mountPaths:    make(map[registry.ID]string),
		middlewareFac: middleware,
	}, nil
}

// SetHandlerFunc sets the server-level handler function
func (s *ServerService) SetHandlerFunc(handler http.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlerFunc = handler
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
			m, err := s.middlewareFac.CreateMiddleware(mw, cfg.Options)
			if err != nil {
				return fmt.Errorf("failed to create middleware %s: %w", mw, err)
			}

			middlewares = append(middlewares, m)
		}
	}

	// Convert post-match middleware config to actual middleware functions
	postMiddlewares := make([]func(http.Handler) http.Handler, 0, len(cfg.PostMiddleware))
	if s.middlewareFac != nil {
		for _, mw := range cfg.PostMiddleware {
			m, err := s.middlewareFac.CreateMiddleware(mw, cfg.PostOptions)
			if err != nil {
				return fmt.Errorf("failed to create post-match middleware %s: %w", mw, err)
			}

			postMiddlewares = append(postMiddlewares, m)
		}
	}

	return s.routeMgr.AddRouter(id, cfg.Prefix, middlewares, postMiddlewares)
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
func (s *ServerService) Rebuild(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// If handler function is set, don't rebuild router
	if s.handlerFunc != nil {
		return nil
	}

	err := s.routeMgr.Build()
	if err != nil {
		return err
	}

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
	hostConfig := relaysys.HostConfig{
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
	s.host = relaysys.NewHost(ctx, hostConfig)

	s.ctx = ctx

	// Use handler function if set, otherwise use route manager
	var baseHandler http.Handler
	if s.handlerFunc != nil {
		baseHandler = s.handlerFunc
	} else {
		baseHandler = s.routeMgr
	}

	// Wrap handler with per-request FrameContext creation
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create unsealed FrameContext for each HTTP request
		ctx, fc := contextapi.OpenFrameContext(r.Context())
		defer contextapi.ReleaseFrameContext(fc)

		// Set all HTTP-specific metadata in FrameContext in one place
		_ = fc.SetMultiple(
			contextapi.Pair{Key: config.ContextServerID, Value: s.id},
			contextapi.Pair{Key: config.ContextHost, Value: s},
		)

		baseHandler.ServeHTTP(w, r.WithContext(ctx))
	})

	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      handler,
		ReadTimeout:  s.config.Timeouts.ReadTimeout,
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext: func(_ net.Listener) context.Context {
			// Return app-level context only
			return s.ctx
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
			dialCtx, cancel := context.WithTimeout(ctx, time.Second)
			dialer := &net.Dialer{}
			conn, err := dialer.DialContext(dialCtx, "tcp", s.config.Addr)
			cancel()
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

// Implement Host interface methods by delegating to embedded host

// Attach implements relay.Host Attach method
func (s *ServerService) Attach(pid relay.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return nil, fmt.Errorf("server host not initialized")
	}

	return s.host.Attach(pid, ch)
}

// Detach implements relay.Host Detach method
func (s *ServerService) Detach(pid relay.PID) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.host == nil {
		return
	}

	s.host.Detach(pid)
}

// Send implements relay.Host Send method
func (s *ServerService) Send(pkg *relay.Package) error {
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
