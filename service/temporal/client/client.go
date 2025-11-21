package client

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	api "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/supervisor"
	"go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Client wraps a Temporal SDK client with lifecycle management
type Client struct {
	id     registry.ID
	log    *zap.Logger
	client client.Client
	config *api.ClientConfig
	closed atomic.Bool
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

// NewClient creates a new wrapped Temporal client
func NewClient(id registry.ID, logger *zap.Logger, temporalClient client.Client, config *api.ClientConfig) *Client {
	return &Client{
		id:     id,
		log:    logger,
		client: temporalClient,
		config: config,
	}
}

// Start initializes and starts the client with health checks
func (c *Client) Start(ctx context.Context) (<-chan any, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("client is closed")
	}

	statusCh := make(chan any, 1)

	// Test connection immediately
	_, err := c.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	if err != nil {
		c.log.Warn("initial health check failed", zap.Error(err))
		statusCh <- supervisor.Failed
	} else {
		statusCh <- supervisor.Running
	}

	// Start health check goroutine if enabled
	if c.config.HealthCheck.Enabled {
		healthCtx, cancel := context.WithCancel(context.Background())
		c.cancel = cancel
		c.wg.Add(1)

		go c.healthCheckLoop(healthCtx, statusCh)
	}

	return statusCh, nil
}

// Stop gracefully shuts down the client
func (c *Client) Stop(ctx context.Context) error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil // Already stopped
	}

	c.log.Info("stopping temporal client", zap.String("id", c.id.String()))

	// Cancel health check goroutine
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for health check goroutine to finish (with timeout from context)
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-ctx.Done():
		c.log.Warn("timeout waiting for health check goroutine to finish")
	}

	// Close the Temporal client
	c.client.Close()

	c.log.Info("temporal client stopped", zap.String("id", c.id.String()))
	return nil
}

// Acquire returns a resource handle for this client
func (c *Client) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("client is closed")
	}

	c.wg.Add(1)
	return &ClientResource{
		client: c.client,
		prefix: c.config.TQPrefix,
		wg:     &c.wg,
	}, nil
}

// healthCheckLoop periodically checks the Temporal connection health
func (c *Client) healthCheckLoop(ctx context.Context, statusCh chan<- any) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := c.client.CheckHealth(checkCtx, &client.CheckHealthRequest{})
			cancel()

			if err != nil {
				c.log.Warn("health check failed",
					zap.String("id", c.id.String()),
					zap.Error(err))
				statusCh <- supervisor.Failed
			} else {
				statusCh <- supervisor.Running
			}
		}
	}
}

// GetTaskQueueName applies the configured prefix to a task queue name
func (c *Client) GetTaskQueueName(queueName string) string {
	if c.config.TQPrefix == "" {
		return queueName
	}
	return c.config.TQPrefix + queueName
}

// ClientResource provides access to the underlying Temporal client
type ClientResource struct {
	client   client.Client
	prefix   string
	wg       *sync.WaitGroup
	released atomic.Bool
}

// Get returns the underlying Temporal client
func (r *ClientResource) Get() (any, error) {
	if r.released.Load() {
		return nil, fmt.Errorf("resource has been released")
	}
	return r.client, nil
}

// GetTaskQueueName applies the configured prefix to a task queue name
func (r *ClientResource) GetTaskQueueName(queueName string) string {
	if r.prefix == "" {
		return queueName
	}
	return r.prefix + queueName
}

// Release decrements the resource wait group
func (r *ClientResource) Release() {
	if !r.released.CompareAndSwap(false, true) {
		return // Already released
	}
	r.wg.Done()
}
