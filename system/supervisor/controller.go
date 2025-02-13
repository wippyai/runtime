package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/backoff"
)

type controlAction int

const (
	controlStart controlAction = iota
	controlStop
	controlFailed
	controlExit
)

type Controllable interface {
	Start() error
	Stop() error
}

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
	ops chan controlOp
}

// NewController creates a new service lifecycle controller with the specified configuration.
// It manages service state transitions and handles retry policies for failed services.
func NewController(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.LifecycleConfig,
	onStateChange func(status supervisor.Status, details any),
) *Controller {
	ctrl := &Controller{
		service:       service,
		config:        config,
		state:         newServiceState(),
		onStateChange: onStateChange,
		root:          ctx,
		ops:           make(chan controlOp, 10),
	}

	ctrl.ops = make(chan controlOp, 10)
	ctrl.ctx, ctrl.cancel = context.WithCancel(ctx)
	go ctrl.supervise()

	return ctrl
}

// Start initiates the service and transitions it to the running state.
// It returns an error if the service fails to start or if the controller is stopped.
func (c *Controller) Start() error {
	c.state.setDesiredStatus(supervisor.Running)
	return c.runCommand(controlOp{kind: controlStart})
}

// Stop gracefully stops the service and transitions it to the stopped state.
// It returns an error if the service fails to stop or if the controller is stopped.
func (c *Controller) Stop() error {
	c.state.setDesiredStatus(supervisor.Stopped)
	return c.runCommand(controlOp{kind: controlStop})
}

func (c *Controller) CanRestart() bool {
	return c.state.getDesiredStatus() != supervisor.Exited
}

func (c *Controller) runCommand(op controlOp) error {
	op.result = make(chan error, 1)
	select {
	case c.ops <- op:
		select {
		case err := <-op.result:
			if err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("failed to stop service: %w", err)
			}
			return err
		case <-c.ctx.Done():
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}
	case <-c.ctx.Done():
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}

func (c *Controller) supervise() {
	var startCh chan<- error
	var exitCh chan any

	var ctx context.Context
	var cancel context.CancelFunc

	respondStart := func(err error) {
		if startCh != nil {
			select {
			case startCh <- err:
			case <-c.root.Done():
			}
			startCh = nil
		}
	}

	respondAndCancel := func(err error) {
		respondStart(err)
		if cancel != nil {
			cancel()
			cancel = nil
		}
	}

	for {
		select {
		case <-c.ctx.Done():
			if cancel != nil {
				cancel()
				cancel = nil
			}

			if startCh != nil {
				select {
				case startCh <- context.Canceled:
				case <-c.root.Done():
				}
			}

			// we are done
			return

		case op := <-c.ops:
			var err error
			switch op.kind {
			case controlStop:
				if ctx == nil {
					// nothing to stop
					break
				}

				err = c.tryStop(ctx)

				// graceful shutdown failed, force exit
				if cancel != nil {
					cancel()
					cancel = nil
				}

				if exitCh != nil {
					// wait for monitor to exit
					select {
					case <-exitCh:
					case <-c.ctx.Done():
					}
					exitCh = nil
				}

				respondAndCancel(context.Canceled) // for active start command if any

			case controlExit:
				c.updateState(supervisor.Exited, nil)
				respondAndCancel(context.Canceled) // for active start command if any

				if cancel != nil {
					cancel()
					cancel = nil
				}
			case controlFailed:
				c.updateState(supervisor.Failed, "unexpected failure")
				if c.state.getDesiredStatus() == supervisor.Running {
					// the service suddenly failed, retry
					go func() {
						select {
						case c.ops <- controlOp{kind: controlStart}:
						case <-c.ctx.Done():
						}
					}()
				}
				continue
			case controlStart:
				if c.state.getCurrentStatus() == supervisor.Running {
					break
				}

				ctx, cancel = context.WithCancel(c.ctx)
				detailsCh, sErr := c.tryStart(ctx, cancel)

				if sErr != nil {
					if startCh == nil && op.result != nil {
						startCh = op.result
						op.result = nil
					}

					if isTerminalError(sErr) {
						c.updateState(supervisor.Exited, sErr)
						respondAndCancel(sErr)
						break
					}

					if c.state.getDesiredStatus() != supervisor.Running {
						respondStart(context.Canceled)
						break
					}

					// Handle non-terminal errors with retry logic
					attempt := c.state.incRetryCount()
					if int(attempt) >= c.config.RetryPolicy.MaxAttempts {
						err = context.DeadlineExceeded
						respondStart(err)
						break
					}

					// Schedule retry
					go c.tryRetry(int(attempt))
					break
				}

				exitCh = make(chan any, 1)
				go c.monitor(ctx, exitCh, detailsCh)
				respondStart(nil)
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

func (c *Controller) monitor(ctx context.Context, exitCh chan<- any, detailsCh <-chan any) {
	defer close(exitCh)
	for {
		select {
		case <-ctx.Done():
			return
		case details, ok := <-detailsCh:
			if !ok {
				if c.state.getDesiredStatus() == supervisor.Running {
					select {
					case c.ops <- controlOp{kind: controlFailed}:
						// immediate retry attempt
					case <-ctx.Done():
					}
				}
				return
			}

			if err, isErr := details.(error); isErr && isTerminalError(err) {
				select {
				case c.ops <- controlOp{kind: controlExit}:
				case <-ctx.Done():
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

func (c *Controller) tryStart(ctx context.Context, cancel context.CancelFunc) (<-chan any, error) {
	if c.state.getCurrentStatus() != supervisor.Failed {
		// reset retry count if this is a fresh start
		c.state.resetRetryCount()
	}

	c.updateState(
		supervisor.Starting,
		fmt.Sprintf("attempt %d", c.state.getRetryCount()+1),
	)

	// Launch the service in a goroutine
	resultCh := make(chan struct {
		ch  <-chan any
		err error
	}, 1)

	go func() {
		ch, err := c.service.Start(ctx)
		select {
		case resultCh <- struct {
			ch  <-chan any
			err error
		}{ch, err}:
		case <-ctx.Done():
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
		cancel()
		c.updateState(supervisor.Failed, "start timeout")
		return nil, errors.New("service start timed out")

	case <-c.ctx.Done():
		c.updateState(supervisor.Exited, "controller exited")
		return nil, supervisor.ErrExit
	}
}

func (c *Controller) tryStop(ctx context.Context) error {
	if c.state.getCurrentStatus() == supervisor.Stopped || c.state.getCurrentStatus() == supervisor.Exited {
		return nil
	}

	c.updateState(supervisor.Stopping, nil)

	resultCh := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(ctx, c.config.StopTimeout)
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

	select {
	case <-time.After(delay):
		select {
		case c.ops <- controlOp{kind: controlStart, attempt: attempt}:
		case <-c.ctx.Done():
		}
	case <-c.ctx.Done():
	}
}

// State returns the current state of the service, including its status,
// desired status, and retry count.
func (c *Controller) State() State {
	return c.state.publicState()
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	c.state.updateState(status, details)
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}
