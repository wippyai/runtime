package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	config "github.com/ponyruntime/pony/api/server/http"
	"github.com/ponyruntime/pony/server/http/router"
)

// Server manages a single HTTP server instance and its associated router
type Server struct {
	config config.ServerConfig
	router *router.Router
	server *http.Server
	mu     sync.RWMutex
}

// NewServer creates a new Server instance with the given configuration and handler
func NewServer(config config.ServerConfig, handler http.HandlerFunc) *Server {
	return &Server{
		config: config,
		router: router.NewRouter(handler),
	}
}

// Router returns the underlying router instance for configuration
func (s *Server) Router() *router.Router {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.router
}

// Serve starts the HTTP server and blocks until the context is canceled
func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	s.server = &http.Server{
		Addr:         s.config.Addr,
		Handler:      s.router,
		ReadTimeout:  s.config.HTTP.ReadTimeout,
		WriteTimeout: s.config.HTTP.WriteTimeout,
		IdleTimeout:  s.config.HTTP.IdleTimeout,
	}
	s.mu.Unlock()

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
		close(serverErr)
	}()

	// Wait for either context cancellation or server error
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		// Give the server a grace period to shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.Stop(shutdownCtx)
	}
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// UpdateConfig updates the server configuration
func (s *Server) UpdateConfig(config config.ServerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}
