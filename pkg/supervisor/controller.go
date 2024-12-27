package supervisor

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/supervisor"
	"sync"
	"time"
)

type State struct {
	Status     supervisor.Status
	Details    any
	Desired    supervisor.Status
	RetryCount int32
	LastUpdate time.Time
}

// Controller manages the lifecycle of a service
type Controller struct {
	// Core dependencies
	service supervisor.Service
	config  supervisor.LifecycleConfig

	// Lifecycle management
	rootCtx     context.Context
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	supervising bool
	mu          sync.Mutex

	// State management
	state         *internalState
	transitions   chan stateTransition
	onStateChange func(supervisor.Status, any)
}

type stateTransition struct {
	target supervisor.Status
	result chan error
}

// NewController creates a new service controller
func NewController(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.LifecycleConfig,
	onStateChange func(status supervisor.Status, details any),
) *Controller {
	return &Controller{
		service:       service,
		config:        config,
		state:         newServiceState(),
		transitions:   make(chan stateTransition, 1),
		onStateChange: onStateChange,
		rootCtx:       ctx,
	}
}

// Start initializes and starts the service controller
func (c *Controller) Start() error {
	c.mu.Lock()
	if !c.supervising {
		c.initializeController()
	}
	c.mu.Unlock()

	return c.transitionTo(supervisor.Running)
}

// Stop gracefully shuts down the service controller
func (c *Controller) Stop() error {
	err := c.transitionTo(supervisor.Stopped)
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to transition to stopped: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.supervising {
		c.shutdownController()
	}

	return nil
}

// State returns the current public state of the controller
func (c *Controller) State() State {
	return c.state.publicState()
}

// Private methods for lifecycle management

func (c *Controller) initializeController() {
	c.ctx, c.cancel = context.WithCancel(c.rootCtx)
	c.transitions = make(chan stateTransition, 1)
	c.wg.Add(1)
	go c.supervise()
	c.supervising = true
}

func (c *Controller) shutdownController() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	close(c.transitions)
	c.supervising = false
	c.ctx = nil
	c.cancel = nil
}

// Core supervision logic

func (c *Controller) supervise() {
	defer c.wg.Done()

	for {
		select {
		case transition, ok := <-c.transitions:
			if !ok {
				return
			}
			err := c.handleTransition(transition.target)
			select {
			case transition.result <- err:
			default:
			}
		case <-c.ctx.Done():
			_ = c.tryStop()
			return
		}
	}
}

func (c *Controller) handleTransition(desired supervisor.Status) error {
	if !c.state.setDesiredStatus(desired) {
		return nil
	}

	switch desired {
	case supervisor.Running:
		if c.state.getCurrentStatus() != supervisor.Running {
			return c.startService()
		}
	case supervisor.Stopped:
		return c.tryStop()
	}

	return nil
}

// Lifecycle lifecycle management

func (c *Controller) startService() error {
	ctx, cancel := context.WithCancel(c.ctx)
	c.state.setContext(ctx, cancel)

	if err := c.tryStart(nil); err != nil {
		c.updateState(supervisor.Failed, err)
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (c *Controller) tryStart(lastErr error) error {
	for attempt := 1; ; attempt++ {
		if err := c.attemptStart(attempt); err == nil {
			return nil
		} else {
			lastErr = err
			if !c.shouldRetry(attempt) {
				return fmt.Errorf("failed to start service after %d attempts: %w", attempt, lastErr)
			}
			time.Sleep(c.config.RetryPolicy.InitialDelay)
		}
	}
}

func (c *Controller) attemptStart(attempt int) error {
	c.updateState(
		supervisor.Starting,
		fmt.Sprintf("attempt %d", attempt-1),
	)

	detailsCh, err := c.service.Start(c.ctx)
	if err != nil {
		c.updateState(supervisor.Failed, err)
		return err
	}
	c.updateState(supervisor.Running, nil) // todo: do we need this or move to convention?

	c.wg.Add(1)
	go c.monitorService(detailsCh)

	return nil
}

func (c *Controller) tryStop() error {
	if c.state.getCurrentStatus() == supervisor.Stopped {
		return nil
	}

	c.updateState(supervisor.Stopping, nil)

	return c.gracefulShutdown()
}

func (c *Controller) gracefulShutdown() error {
	shutdownCtx, cancel := context.WithTimeout(c.rootCtx, c.config.StopTimeout)
	defer cancel()

	err := c.executeShutdown(shutdownCtx)
	c.updateState(supervisor.Stopped, nil)

	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

func (c *Controller) executeShutdown(ctx context.Context) error {
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		errCh <- c.service.Stop(ctx)
	}()

	select {
	case err := <-errCh:
		<-done // normal shutdown
		return err

	case <-ctx.Done():
		// we are shutting down with dead context, let stop goroutine finish but shorter timeout
		stopCtx, cancel := context.WithTimeout(context.Background(), c.config.StopTimeout/2)
		defer cancel()

		select {
		case <-done:
			select {
			case err := <-errCh:
				return err
			default:
				return fmt.Errorf("service stop interrupted: %w", ctx.Err())
			}
		case <-stopCtx.Done():
			return fmt.Errorf("service stop timeout after %v", c.config.StopTimeout)
		}
	}
}

// Lifecycle monitoring and recovery

func (c *Controller) monitorService(detailsCh <-chan any) {
	defer c.wg.Done()

	ctx := c.state.getContext()
	for {
		select {
		case details, ok := <-detailsCh:
			if !ok {
				if c.state.getDesiredStatus() == supervisor.Running {
					c.handleError(fmt.Errorf("service ended unexpectedly"))
				}
				return
			}
			status, details := c.state.updateDetails(details)
			if c.onStateChange != nil {
				c.onStateChange(status, details)
			}
		case <-ctx.Done():
			if c.state.getDesiredStatus() == supervisor.Running {
				c.updateState(supervisor.Stopped, nil)
			}
			return
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Controller) handleError(err error) {
	c.updateState(supervisor.Failed, err)
	if c.state.canRecover(c.config.RetryPolicy.MaxAttempts, c.ctx) {
		go c.recoverService(err)
	}
}

// Helper methods

func (c *Controller) shouldRetry(attempt int) bool {
	if c.ctx.Err() != nil || attempt >= c.config.RetryPolicy.MaxAttempts {
		return false
	}
	return true
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	c.state.updateState(status, details)
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}

func (c *Controller) recoverService(initialErr error) {
	if err := c.tryStart(initialErr); err != nil {
		c.updateState(supervisor.Failed, err)
	}
}

func (c *Controller) transitionTo(status supervisor.Status) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ctx == nil {
		return fmt.Errorf("supervisor is not started")
	}

	if err := c.ctx.Err(); err != nil {
		return fmt.Errorf("supervisor is stopped: %w", err)
	}

	return c.sendTransition(status)
}

func (c *Controller) sendTransition(status supervisor.Status) error {
	result := make(chan error, 1)

	select {
	case c.transitions <- stateTransition{target: status, result: result}:
		select {
		case err := <-result:
			return err
		case <-c.ctx.Done():
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}
	case <-c.ctx.Done():
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}
