package http

import (
	"context"
	"errors"
	"fmt"
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
)

// Server manages a single Timeouts service instance and its associated router
type Server struct {
	config config.ServerConfig
	router *router.Router
	server *http.Server
	mu     sync.RWMutex

	// Internal status channel
	statusChan chan any
}

// NewServer creates a new Server instance with the given configuration and handler
func NewServer(config config.ServerConfig, handler http.HandlerFunc) *Server {
	return &Server{
		config: config,
		router: router.NewRouter(handler),
	}
}

// ensureRunning verifies if the server is listening on its configured address
func (s *Server) ensureRunning(ctx context.Context) error {
	timeout := time.After(BootTimeout)
	ticker := time.NewTicker(CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case err, ok := <-s.statusChan:
			if !ok {
				return errors.New("service failed to start")
			}

			if e, ok := err.(error); ok {
				return e
			}
		case <-timeout:
			return errors.New("service failed to start within timeout")
		case <-ctx.Done():
			return ctx.Err()
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
		ReadTimeout:  s.config.Timeouts.ReadTimeout, // todo: remove nested one?
		WriteTimeout: s.config.Timeouts.WriteTimeout,
		IdleTimeout:  s.config.Timeouts.IdleTimeout,
		BaseContext:  func(listener net.Listener) context.Context { return ctx },
	}
	s.server.RegisterOnShutdown(func() {
		close(s.statusChan)
	})

	s.statusChan = make(chan any, 2) // 1 for initial boot message and extra for shutdown
	s.mu.Unlock()

	// Start service in a goroutine
	go func() {
		err := s.server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.statusChan <- err
		}
	}()

	// Check if service starts successfully
	if err := s.ensureRunning(ctx); err != nil {
		return nil, err
	}

	// we are running!
	s.statusChan <- fmt.Sprint("service listening on ", s.config.Addr)

	return s.statusChan, nil
}

// Stop implements the ServerManager interface
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}

	return nil
}

// UpdateConfig updates the service configuration
func (s *Server) UpdateConfig(config config.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = config

	// no changes to the server instance
}
