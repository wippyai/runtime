package client

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/service/temporal"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

const (
	// HealthCheckInterval defines how often to verify client connectivity
	HealthCheckInterval = 1 * time.Minute
)

// Client implements supervisor.Service interface for Temporal client
type Client struct {
	mu     sync.RWMutex
	log    *zap.Logger
	id     registry.ID
	config *api.ClientConfig
	client client.Client

	// Internal status channel
	statusChan chan any
	exit       chan struct{}
}

// NewClient creates a new client service instance
func NewClient(logger *zap.Logger, id registry.ID, config *api.ClientConfig) *Client {
	return &Client{
		log:    logger,
		config: config,
	}
}

func (s *Client) ID() registry.ID {
	return s.id
}

// Start implements supervisor.Service interface
func (s *Client) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	if s.client != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("client already started")
	}

	// Create temporal client
	c, err := client.Dial(client.Options{
		HostPort:  s.config.Address,
		Namespace: s.config.Namespace,
		Logger:    newZapLogger(s.log),
		// todo: add other client options from config as needed
		// todo: add context propagation
	})
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	s.client = c
	s.statusChan = make(chan any, 3) // Buffer for status updates
	s.exit = make(chan struct{})
	s.mu.Unlock()

	// verify client connectivity
	if i, err := s.client.WorkflowService().GetSystemInfo(ctx, nil); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("failed to verify client connectivity: %w", err)
	} else {
		s.log.Info("temporal client started")
		s.statusChan <- fmt.Sprintf("client connected to %s, server version: %s, capabilities: %s",
			s.config.Address,
			i.ServerVersion,
			i.GetCapabilities().String(), // todo: expose for consumers
		)
	}

	// Start health check goroutine
	go s.healthCheck(ctx)

	return s.statusChan, nil
}

// Stop implements supervisor.Service interface
func (s *Client) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.client != nil {
		s.client.Close()
		s.client = nil
		close(s.statusChan) // Only close channel when service is fully stopped
		close(s.exit)
		s.log.Info("temporal client stopped")
	}

	return nil
}

// GetClient returns the temporal client instance
func (s *Client) GetClient() (client.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.client == nil {
		return nil, fmt.Errorf("client not started")
	}

	return s.client, nil
}

// GetLifecycleConfig returns supervisor lifecycle configuration
func (s *Client) GetLifecycleConfig() supervisor.LifecycleConfig {
	return supervisor.LifecycleConfig{
		AutoStart: true,
	}
}

// healthCheck periodically verifies client connectivity
func (s *Client) healthCheck(ctx context.Context) {
	ticker := time.NewTicker(HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.exit:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.RLock()
			if s.client == nil {
				s.mu.RUnlock()
				return
			}

			// Attempt to make a simple API call to check connectivity
			// Using namespaces API as it's lightweight
			if _, err := s.client.WorkflowService().GetSystemInfo(ctx, nil); err != nil {
				s.statusChan <- fmt.Sprintf("health check failed: %v", err)
				s.log.Warn("client health check failed", zap.Error(err))
			}
			s.mu.RUnlock()
		}
	}
}
