package http

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5/middleware"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/registry"
	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/api/supervisor"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	BootTimeout   = 30 * time.Second
	CheckInterval = 100 * time.Millisecond
	StatusBuffer  = 10
)

var ContextListener = &contextapi.Key{Name: "listener"}

// ServerService combines HTTP server and router functionality
type ServerService struct {
	config     *config.ServerConfig
	routeMgr   *RouteManager
	server     *http.Server
	mu         sync.RWMutex
	statusChan chan any
	started    bool // Track if server has been started
}

// NewServerService creates a new ServerService instance
func NewServerService(cfg *config.ServerConfig) *ServerService {
	return &ServerService{
		config:     cfg,
		routeMgr:   NewRouteManager(),
		statusChan: make(chan any, StatusBuffer),
	}
}

// UpdateConfig implements Server interface
func (s *ServerService) UpdateConfig(cfg *config.ServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		if s.config.Addr != cfg.Addr {
			return fmt.Errorf("cannot change server address while running")
		}
		s.server.ReadTimeout = cfg.Timeouts.ReadTimeout
		s.server.WriteTimeout = cfg.Timeouts.WriteTimeout
		s.server.IdleTimeout = cfg.Timeouts.IdleTimeout
	}

	s.config = cfg
	return nil
}

// AddRouter adds a new router with its middleware stack
func (s *ServerService) AddRouter(id registry.ID, cfg *config.RouterConfig) error {
	// Convert middleware config to actual middleware functions
	middlewares := make([]func(http.Handler) http.Handler, 0, len(cfg.Middlewares))
	for _, mw := range cfg.Middlewares {
		if fn := s.createMiddleware(mw, cfg.Options); fn != nil {
			middlewares = append(middlewares, fn)
		}
	}

	return s.routeMgr.AddRouter(id, cfg.Prefix, middlewares)
}

// DeleteRouter implements Server interface
func (s *ServerService) DeleteRouter(id registry.ID) error {
	return s.routeMgr.RemoveRouter(id)
}

// AddEndpoint implements Server interface
func (s *ServerService) AddEndpoint(routerID, id registry.ID, path string, method string, handler http.Handler) error {
	return s.routeMgr.AddRoute(routerID, id, method, path, id, handler)
}

// RemoveEndpoint implements Server interface
func (s *ServerService) RemoveEndpoint(routerID, id registry.ID) error {
	return s.routeMgr.RemoveRoute(routerID, id)
}

// Mount implements Server interface
func (s *ServerService) Mount(id registry.ID, path string, handler http.Handler) error {
	return s.routeMgr.Mount(path, handler)
}

// Remove implements Server interface
func (s *ServerService) Remove(id registry.ID) error {
	return s.routeMgr.Unmount(id.String())
}

// Rebuild implements Server interface
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

// Start implements supervisor.Service
func (s *ServerService) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.routeMgr,
		ReadTimeout:  s.config.Timeouts.ReadTimeout,
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext: func(l net.Listener) context.Context {
			return context.WithValue(ctx, ContextListener, l)
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

// Stop implements supervisor.Service
func (s *ServerService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.server = nil
		s.started = false
	}
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
			return fmt.Errorf("startup cancelled: %w", ctx.Err())
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", s.config.Addr, time.Second)
			if err == nil {
				_ = conn.Close()
				return nil
			}
		}
	}
}

// Helper methods for middleware creation
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

// Ensure ServerService implements required interfaces
var (
	_ supervisor.Service = (*ServerService)(nil)
	_ Server             = (*ServerService)(nil)
)
