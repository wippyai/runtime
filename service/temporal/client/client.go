package client

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/resource"
	api "github.com/ponyruntime/pony/api/service/temporal"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.uber.org/zap"
)

// Client implements supervisor.Service interface for Temporal client
// and resource.Provider for client resource acquisition
type Client struct {
	mu       sync.RWMutex
	ctx      context.Context
	log      *zap.Logger
	id       registry.ID
	dc       converter.DataConverter
	config   *api.ClientConfig
	client   client.Client
	tqPrefix string // Task queue prefix for all queues using this client
	closed   atomic.Bool
	wg       sync.WaitGroup // tracks active resource users

	// Health check configuration
	healthInterval time.Duration
	healthEnabled  bool

	// Internal status channel
	statusChan chan any
	exit       chan struct{}
}

// Start implements supervisor.Service interface
func (s *Client) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed.CompareAndSwap(true, false) {
		if s.client != nil {
			// Already started
			return s.statusChan, nil
		}
	}

	// Build client options with our configuration
	options, err := BuildClientOptions(s.config, s.log, s.dc)
	if err != nil {
		return nil, fmt.Errorf("failed to build client options: %w", err)
	}

	// Create temporal client
	c, err := client.Dial(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	s.ctx = ctx
	s.client = c

	// verify client connectivity
	i, err := s.client.WorkflowService().GetSystemInfo(ctx, nil)
	if err != nil {
		// Close the client if we can't connect
		s.client.Close()
		s.client = nil
		return nil, fmt.Errorf("failed to verify client connectivity: %w", err)
	}
	s.log.Info("temporal client started",
		zap.String("address", s.config.Connect.Address),
		zap.String("namespace", s.config.Connect.Namespace),
		zap.String("auth_type", string(s.config.Auth.Type)),
		zap.String("tq_prefix", s.tqPrefix),
		zap.String("server_version", i.ServerVersion),
	)
	s.statusChan <- fmt.Sprintf("client connected to %s, server version: %s",
		s.config.Connect.Address,
		i.ServerVersion,
	)

	// Start health check goroutine if enabled
	if s.healthEnabled && s.healthInterval > 0 {
		s.wg.Add(1)
		go s.healthCheck(ctx)
		s.log.Info("health check routine started",
			zap.Duration("interval", s.healthInterval))
	}

	return s.statusChan, nil
}

// Stop implements supervisor.Service interface
func (s *Client) Stop(ctx context.Context) error {
	s.mu.Lock()

	if s.closed.Load() {
		s.mu.Unlock()
		return nil
	}

	if s.client != nil {
		close(s.exit)
		s.closed.Store(true)
		s.mu.Unlock()

		// Wait for health check and resources with timeout
		done := make(chan struct{})
		go func() {
			s.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All resources released
		case <-ctx.Done():
			s.log.Warn("client stop timed out waiting for resources")
			return ctx.Err()
		}

		// Close the client
		s.mu.Lock()
		if s.client != nil {
			s.client.Close()
			s.client = nil
			s.log.Info("temporal client stopped")
		}
		s.mu.Unlock()
		return nil
	}

	s.mu.Unlock()
	return nil
}

// ID returns the registry ID of the client
func (s *Client) ID() registry.ID {
	return s.id
}

// GetTaskQueueName applies prefix to a task queue name if configured
func (s *Client) GetTaskQueueName(name string) string {
	if s.tqPrefix == "" {
		return name
	}
	// Only apply prefix if it's not already there
	if strings.HasPrefix(name, s.tqPrefix) {
		return name
	}
	return s.tqPrefix + name
}

// GetClient returns the temporal client instance
func (s *Client) GetClient() (client.Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed.Load() || s.client == nil {
		return nil, fmt.Errorf("client not started or is closed")
	}

	return s.client, nil
}

// GetTaskQueuePrefix returns the configured task queue prefix
func (s *Client) GetTaskQueuePrefix() string {
	return s.tqPrefix
}

// healthCheck periodically verifies client connectivity
func (s *Client) healthCheck(ctx context.Context) {
	defer s.wg.Done()

	ticker := time.NewTicker(s.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.exit:
			s.log.Debug("health check routine stopped")
			return
		case <-ctx.Done():
			s.log.Debug("health check routine stopped by context")
			return
		case <-ticker.C:
			s.checkHealth(ctx)
		}
	}
}

// checkHealth verifies the client connection
func (s *Client) checkHealth(ctx context.Context) {
	s.mu.RLock()
	if s.closed.Load() || s.client == nil {
		s.mu.RUnlock()
		return
	}

	// Attempt to make a simple API call to check connectivity
	// Using GetSystemInfo as it's lightweight
	_, err := s.client.WorkflowService().GetSystemInfo(ctx, nil)
	s.mu.RUnlock()

	if err != nil {
		select {
		case s.statusChan <- fmt.Sprintf("health check failed: %v", err):
		default:
		}
		s.log.Warn("client health check failed", zap.Error(err))
	} else {
		s.log.Debug("client health check succeeded")
	}
}

// Acquire implements resource.Provider
func (s *Client) Acquire(
	_ context.Context,
	_ registry.ID,
	mode resource.AccessMode,
) (resource.Resource[any], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed.Load() {
		return nil, fmt.Errorf("client is closed")
	}

	// Only support normal mode for now
	if mode != resource.ModeNormal {
		return nil, fmt.Errorf("unsupported access mode: %v", mode)
	}

	// Track resource usage
	s.wg.Add(1)

	return &clientResource{
		client: s,
		tcl:    s.client,
	}, nil
}

// clientResource represents an acquired client resource
type clientResource struct {
	client *Client
	tcl    client.Client
	closed atomic.Bool
}

// Resource is the resource provided to consumers
type Resource struct {
	Client client.Client
	Prefix string
}

// Get implements resource.Resource
func (r *clientResource) Get() (any, error) {
	if r.closed.Load() {
		return nil, resource.ErrResourceReleased
	}

	return Resource{
		Client: r.tcl,
		Prefix: r.client.tqPrefix,
	}, nil
}

// Release implements resource.Resource
func (r *clientResource) Release() {
	if !r.closed.CompareAndSwap(false, true) {
		return
	}

	r.client.wg.Done()
}

// Ensure Client implements required interfaces
var (
	_ resource.Provider  = (*Client)(nil)
	_ supervisor.Service = (*Client)(nil)
)
