package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/backoff"
)

type controlAction int

const (
	controlStart controlAction = iota
	controlStop
	controlExit
)

type controlOp struct {
	kind    controlAction
	attempt int
	result  chan error
}

// Controller manages the lifecycle of a service
type Controller struct {
	service       supervisor.Service
	config        supervisor.LifecycleConfig
	state         *internalState
	onStateChange func(supervisor.Status, any)

	// controller level context
	root context.Context

	// controller level context, cancellable
	ctx    context.Context
	cancel context.CancelFunc

	// active ops
	active bool
	mu     sync.Mutex
	ops    chan controlOp
}

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
		onStateChange: onStateChange,
		root:          ctx,
		ops:           make(chan controlOp, 10),
	}
}

func (c *Controller) Start() error {
	c.mu.Lock()
	if !c.active {
		c.bootController()
	}
	c.mu.Unlock()

	result := make(chan error, 1)
	select {
	case c.ops <- controlOp{kind: controlStart, result: result}:
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

func (c *Controller) Stop() error {
	c.mu.Lock()
	if !c.active {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	result := make(chan error, 1)
	select {
	case c.ops <- controlOp{kind: controlStop, result: result}:
		select {
		case err := <-result:
			if err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("failed to stop service: %w", err)
			}
		case <-c.root.Done():
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}
	case <-c.root.Done():
		// Already stopping, just wait for cleanup
	}

	select {
	case <-c.ctx.Done():
		return nil
	case <-c.root.Done():
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}

func (c *Controller) supervise() {
	var startCh chan<- error

	for {
		select {
		case <-c.ctx.Done():
			if c.cancel != nil {
				_ = c.tryStop()
			}
			c.shutdown()
			return

		case op := <-c.ops:
			var err error
			switch op.kind {
			case controlStart:
				c.state.setDesiredStatus(supervisor.Running)
				c.bootContext()

				detailsCh, sErr := c.tryStart()

				if sErr != nil {
					if isTerminalError(sErr) {
						if startCh == nil {
							startCh = op.result
							op.result = nil
						}

						c.updateState(supervisor.Exited, sErr)
						if startCh != nil {
							// report to original caller
							select {
							case startCh <- sErr:
							case <-c.root.Done():
							}
							startCh = nil
						}

						if c.cancel != nil {
							c.cancel()
							c.cancel = nil
						}
						break
					}

					// Handle non-terminal errors with retry logic
					attempt := c.state.incRetryCount()
					if int(attempt) >= c.config.RetryPolicy.MaxAttempts {
						err = context.DeadlineExceeded
						if startCh != nil {
							// report to original caller
							select {
							case startCh <- err:
							case <-c.root.Done():
							}
							startCh = nil
						}
						break
					}

					// Schedule retry
					go c.tryRetry(int(attempt))
					if startCh == nil {
						// respond when we ready
						startCh = op.result
						op.result = nil
					}
					break
				}

				go c.monitor(detailsCh)

				if startCh != nil {
					// report to original caller
					select {
					case startCh <- nil:
					case <-c.root.Done():
					}
					startCh = nil
				}

			case controlStop:
				c.state.setDesiredStatus(supervisor.Stopped)
				err = c.tryStop()

				if startCh != nil {
					// report to original caller
					select {
					case startCh <- context.Canceled:
					case <-c.root.Done():
					}
					startCh = nil
				}

				if c.cancel != nil {
					c.cancel()
					c.cancel = nil
				}

			case controlExit:
				c.updateState(supervisor.Exited, nil)

				if startCh != nil {
					// report to original caller
					select {
					case startCh <- context.Canceled:
					case <-c.root.Done():
					}
					startCh = nil
				}

				if c.cancel != nil {
					c.cancel()
					c.cancel = nil
				}
			}

			if op.result != nil {
				select {
				case op.result <- err:
				case <-c.root.Done():
				}
			}
		}
	}
}

func (c *Controller) monitor(detailsCh <-chan any) {
	svcCtx := c.state.getContext()
	for {
		select {
		case <-svcCtx.Done():
			return
		case details, ok := <-detailsCh:
			if !ok {
				if c.state.getDesiredStatus() == supervisor.Running {
					err := fmt.Errorf("service ended unexpectedly")
					c.updateState(supervisor.Failed, err)

					// Schedule retry through supervisor
					select {
					case c.ops <- controlOp{kind: controlStart}:
					case <-c.ctx.Done():
					}
				}
				return
			}

			if err, isErr := details.(error); isErr && isTerminalError(err) {
				select {
				case c.ops <- controlOp{kind: controlExit}:
				case <-c.ctx.Done():
				}
				return
			}

			status, details := c.state.updateDetails(details)
			if c.onStateChange != nil {
				c.onStateChange(status, details)
			}
		}
	}
}

func (c *Controller) tryStart() (<-chan any, error) {
	if c.state.getCurrentStatus() == supervisor.Running {
		return nil, nil
	}

	// Reset retry count if this is a fresh start
	if c.state.getCurrentStatus() != supervisor.Failed {
		c.state.resetRetryCount()
	}

	c.updateState(supervisor.Starting, fmt.Sprintf("attempt %d", c.state.getRetryCount()))

	// Start the service in a goroutine
	resultCh := make(chan struct {
		ch  <-chan any
		err error
	}, 1)

	go func() {
		ch, err := c.service.Start(c.state.getContext())
		select {
		case resultCh <- struct {
			ch  <-chan any
			err error
		}{ch, err}:
		case <-c.state.getContext().Done():
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			if isTerminalError(result.err) {
				return nil, result.err
			}
			// Just update state and return error, let supervise handle retry
			c.updateState(supervisor.Failed, result.err)
			return nil, result.err
		}

		c.updateState(supervisor.Running, nil)
		return result.ch, nil

	case <-time.After(c.config.StartTimeout):
		c.state.cancelContext()
		c.updateState(supervisor.Failed, "start timeout")
		return nil, context.DeadlineExceeded

	case <-c.ctx.Done():
		c.updateState(supervisor.Exited, "controller exited")
		return nil, supervisor.ExitErr
	}
}

func (c *Controller) tryStop() error {
	if c.state.getCurrentStatus() == supervisor.Stopped {
		return nil
	}

	c.updateState(supervisor.Stopping, nil)

	resultCh := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(c.root, c.config.StopTimeout)
	defer cancel()

	go func() {
		err := c.service.Stop(stopCtx)
		select {
		case resultCh <- err:
		case <-stopCtx.Done():
		}
	}()

	select {
	case err := <-resultCh:
		c.updateState(supervisor.Stopped, err)
		return err
	case <-stopCtx.Done():
		c.updateState(supervisor.Failed, fmt.Sprintf("stop timeout after %v", c.config.StopTimeout))
		return fmt.Errorf("service stop timed out after %v", c.config.StopTimeout)
	}
}

func (c *Controller) tryRetry(attempt int) {
	if attempt >= c.config.RetryPolicy.MaxAttempts {
		return
	}

	bf := backoff.NewCalculator(c.config.RetryPolicy)
	delay := bf.NextInterval()

	svcCtx := c.state.getContext()

	select {
	case <-time.After(delay):
		select {
		case c.ops <- controlOp{
			kind:    controlStart,
			attempt: attempt,
		}:
		case <-svcCtx.Done():
		}
	case <-svcCtx.Done():
	}
}

func (c *Controller) bootController() {
	c.ops = make(chan controlOp, 10)
	c.active = true
	c.ctx, c.cancel = context.WithCancel(c.root)

	go c.supervise()
}

func (c *Controller) bootContext() {
	ctx, cancel := context.WithCancel(c.ctx)
	c.state.setContext(ctx, cancel)
}

func (c *Controller) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.active = false
}

func (c *Controller) State() State {
	return c.state.publicState()
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	c.state.updateState(status, details)
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}
