package http

import (
	"context"
	"errors"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"net"
	"net/http"
	"sync"
	"time"

	config "github.com/ponyruntime/pony/api/service/http"
	"github.com/ponyruntime/pony/service/http/router"
)

const (
	// BootTimeout defines how long to wait for service to start
	BootTimeout = 30 * time.Second
	// CheckInterval defines how often to check service status during boot
	CheckInterval = 100 * time.Millisecond
	// StatusBuffer defines buffer size for status channel
	StatusBuffer = 10
)

var ContextListener = &contextapi.Key{Name: "listener"}

// Server manages a single HTTP service instance and its associated router
type Server struct {
	config config.ServerConfig
	router *router.Router
	server *http.Server
	mu     sync.RWMutex

	// Immutable channels for status updates and errors
	statusChan chan any
}

// NewServer creates a new Server instance with the given configuration and handler
func NewServer(config config.ServerConfig, handler http.HandlerFunc) *Server {
	return &Server{
		config:     config,
		router:     router.NewRouter(handler),
		statusChan: make(chan any, StatusBuffer), // Created once, never closed
	}
}

// ensureRunning verifies if the server is listening on its configured address
func (s *Server) ensureRunning(ctx context.Context) error {
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

// Start implements the ServerManager interface
func (s *Server) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.router,
		ReadTimeout:  s.config.Timeouts.ReadTimeout,
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext: func(l net.Listener) context.Context {
			// Inherit parent context and add listener info
			return context.WithValue(ctx, ContextListener, l)
		},
	}
	s.mu.Unlock()

	// Launch service in a goroutine
	go func() {
		err := s.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case s.statusChan <- fmt.Errorf("server error: %w", err):
			default:
				// Log could be added here if status channel is full
			}
		}
	}()

	// Check if service starts successfully
	if err := s.ensureRunning(ctx); err != nil {
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
				// Log could be added here if status channel is full
			}
		}
	}()

	// Signal successful start
	select {
	case s.statusChan <- fmt.Sprintf("service listening on %s", s.config.Addr):
	default:
		// Log could be added here if status channel is full
	}

	return s.statusChan, nil
}

// Stop implements the ServerManager interface
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		if err := s.server.Shutdown(ctx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}
		s.server = nil
	}
	return nil
}

// UpdateConfig updates the service configuration
// Note: Changes will only take effect on the next Start
func (s *Server) UpdateConfig(config config.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}
