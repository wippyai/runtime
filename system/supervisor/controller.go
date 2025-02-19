package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	attempt int32
	result  chan error
}

// Controller manages the lifecycle of a service.
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

	// runStart records the time when the service started successfully.
	// It is used to determine whether the service has run for long enough
	// (i.e. reached the stable threshold) to reset the retry counter.
	runStart time.Time
}

// NewController creates a new service lifecycle controller with the specified configuration.
// It manages service state transitions and handles retry policies for failed services.
func NewController(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.LifecycleConfig,
	onStateChange func(status supervisor.Status, details any),
) *Controller {
	log.Printf("NewController: Creating new controller for service")
	ctrl := &Controller{
		service:       service,
		config:        config,
		state:         newServiceState(),
		onStateChange: onStateChange,
		root:          ctx,
		ops:           make(chan controlOp, 10),
	}
	ctrl.ctx, ctrl.cancel = context.WithCancel(ctx)
	go ctrl.supervise()
	log.Printf("NewController: Controller started supervision")
	return ctrl
}

// Start initiates the service and transitions it to the running state.
func (c *Controller) Start() error {
	log.Printf("Start: Setting desired status to Running and sending controlStart op")
	c.state.setDesiredStatus(supervisor.Running)
	return c.runCommand(controlOp{kind: controlStart})
}

// Stop gracefully stops the service and transitions it to the stopped state.
func (c *Controller) Stop() error {
	log.Printf("Stop: Setting desired status to Stopped and sending controlStop op")
	c.state.setDesiredStatus(supervisor.Stopped)
	return c.runCommand(controlOp{kind: controlStop})
}

func (c *Controller) CanRestart() bool {
	return c.state.getDesiredStatus() != supervisor.Exited
}

func (c *Controller) runCommand(op controlOp) error {
	log.Printf("runCommand: Received op of kind %d (attempt: %d)", op.kind, op.attempt)
	op.result = make(chan error, 1)
	select {
	case c.ops <- op:
		log.Printf("runCommand: op sent successfully")
		select {
		case err := <-op.result:
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("runCommand: op returned error: %v", err)
				return fmt.Errorf("failed to stop service: %w", err)
			}
			log.Printf("runCommand: op completed successfully")
			return err
		case <-c.ctx.Done():
			log.Printf("runCommand: controller context done")
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}
	case <-c.ctx.Done():
		log.Printf("runCommand: controller context done before sending op")
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}

func (c *Controller) supervise() {
	log.Printf("supervise: Starting supervision loop")
	var startCh chan<- error
	var exitCh chan any
	var ctx context.Context
	var cancel context.CancelFunc

	respondStart := func(err error) {
		log.Printf("respondStart: Responding with err: %v", err)
		if startCh != nil {
			select {
			case startCh <- err:
				log.Printf("respondStart: Sent response")
			case <-c.root.Done():
				log.Printf("respondStart: Root context done")
			}
			startCh = nil
		}
	}

	respondAndCancel := func(err error) {
		log.Printf("respondAndCancel: Responding and cancelling with err: %v", err)
		respondStart(err)
		if cancel != nil {
			cancel()
			cancel = nil
			log.Printf("respondAndCancel: Cancelled context")
		}
	}

	for {
		select {
		case <-c.ctx.Done():
			log.Printf("supervise: Controller context done, exiting supervision loop")
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
			return

		case op := <-c.ops:
			log.Printf("supervise: Received op of kind %d", op.kind)
			var err error
			switch op.kind {
			case controlStop:
				log.Printf("supervise: Handling controlStop")
				if ctx == nil {
					log.Printf("supervise: No active context to stop")
					break
				}
				err = c.tryStop(ctx)
				log.Printf("supervise: tryStop returned err: %v", err)
				if cancel != nil {
					cancel()
					cancel = nil
					log.Printf("supervise: Cancelled active context after stop")
				}
				if exitCh != nil {
					select {
					case <-exitCh:
						log.Printf("supervise: Monitor exited")
					case <-c.ctx.Done():
						log.Printf("supervise: Controller context done while waiting for monitor")
					}
					exitCh = nil
				}
				respondAndCancel(context.Canceled) // for active start command if any

			case controlExit:
				log.Printf("supervise: Handling controlExit, updating state to Exited")
				c.updateState(supervisor.Exited, nil)
				respondAndCancel(context.Canceled)
				if cancel != nil {
					cancel()
					cancel = nil
					log.Printf("supervise: Cancelled context after exit")
				}

			case controlFailed:
				attempt := c.state.incRetryCount()
				c.updateState(supervisor.Failed, c.state.details)
				if c.state.getDesiredStatus() == supervisor.Running {
					if !c.runStart.IsZero() && time.Since(c.runStart) >= c.config.StableThreshold {
						c.state.resetRetryCount()
						attempt = 1
					}
					c.runStart = time.Time{}
					if c.config.RetryPolicy.MaxAttempts != 0 && int(attempt) >= c.config.RetryPolicy.MaxAttempts {
						log.Printf("Max attempts reached; not scheduling further retries")
					} else {
						// Instead of immediate scheduling, call tryRetry to delay the next start.
						log.Printf("Scheduling delayed retry via tryRetry, attempt %d", attempt)
						go c.tryRetry(attempt)
					}
				}
				continue

			case controlStart:
				log.Printf("supervise: Handling controlStart op")
				if c.state.getCurrentStatus() == supervisor.Running {
					log.Printf("supervise: Service already running; ignoring controlStart")
					break
				}
				ctx, cancel = context.WithCancel(c.ctx)
				detailsCh, sErr := c.tryStart(ctx, cancel)
				if sErr != nil {
					log.Printf("supervise: tryStart returned error: %v", sErr)
					if startCh == nil && op.result != nil {
						startCh = op.result
						op.result = nil
					}
					if isTerminalError(sErr) {
						log.Printf("supervise: Terminal error in tryStart, updating state to Exited")
						c.updateState(supervisor.Exited, sErr)
						respondAndCancel(sErr)
						break
					}
					if c.state.getDesiredStatus() != supervisor.Running {
						respondStart(context.Canceled)
						break
					}
					// For errors returned directly from tryStart, increment retry count here.
					attempt := c.state.incRetryCount()
					log.Printf("supervise: Non-terminal error in tryStart, attempt %d", attempt)
					if c.config.RetryPolicy.MaxAttempts != 0 && int(attempt) >= c.config.RetryPolicy.MaxAttempts {
						err = context.DeadlineExceeded
						log.Printf("supervise: Max attempts reached; responding with DeadlineExceeded")
						respondStart(err)
						break
					}
					log.Printf("supervise: Scheduling retry via tryRetry for attempt %d", attempt)
					go c.tryRetry(attempt)
					break
				}
				exitCh = make(chan any, 1)
				log.Printf("supervise: Service started successfully; launching monitor")
				go c.monitor(ctx, exitCh, detailsCh)
				respondStart(nil)
			}

			if op.result != nil {
				select {
				case op.result <- err:
					log.Printf("supervise: Sent op result: %v", err)
				case <-c.root.Done():
					log.Printf("supervise: Root context done while sending op result")
				}
			}
		}
	}
}

func (c *Controller) monitor(ctx context.Context, exitCh chan<- any, detailsCh <-chan any) {
	log.Printf("monitor: Starting monitor loop")
	defer func() {
		log.Printf("monitor: Exiting monitor loop")
		close(exitCh)
	}()
	for {
		select {
		case <-ctx.Done():
			log.Printf("monitor: Context done")
			return
		case details, ok := <-detailsCh:
			if !ok {
				log.Printf("monitor: Details channel closed")
				if c.state.getDesiredStatus() == supervisor.Running {
					log.Printf("monitor: Service desired status Running; sending controlFailed for immediate retry")
					select {
					case c.ops <- controlOp{kind: controlFailed}:
						log.Printf("monitor: controlFailed op enqueued")
					case <-ctx.Done():
						log.Printf("monitor: Context done while enqueuing controlFailed")
					}
				}
				return
			}
			log.Printf("monitor: Received details: %v", details)
			if err, isErr := details.(error); isErr && isTerminalError(err) {
				log.Printf("monitor: Terminal error encountered in details: %v", err)
				select {
				case c.ops <- controlOp{kind: controlExit}:
					log.Printf("monitor: controlExit op enqueued")
				case <-ctx.Done():
					log.Printf("monitor: Context done while enqueuing controlExit")
				}
				return
			}
			status, updatedDetails := c.state.updateDetails(details)
			log.Printf("monitor: Updated state: %v, details: %v", status, updatedDetails)
			if c.onStateChange != nil {
				c.onStateChange(status, updatedDetails)
			}
		}
	}
}

func (c *Controller) tryStart(ctx context.Context, cancel context.CancelFunc) (<-chan any, error) {
	log.Printf("tryStart: Attempting to start service (attempt %d)", c.state.getRetryCount()+1)
	c.updateState(
		supervisor.Starting,
		fmt.Sprintf("attempt %d", c.state.getRetryCount()+1),
	)

	resultCh := make(chan struct {
		ch  <-chan any
		err error
	}, 1)

	go func() {
		log.Printf("tryStart: Calling service.Start")
		ch, err := c.service.Start(ctx)
		select {
		case resultCh <- struct {
			ch  <-chan any
			err error
		}{ch, err}:
			log.Printf("tryStart: service.Start returned, err: %v", err)
		case <-ctx.Done():
			log.Printf("tryStart: Context done before sending result")
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			log.Printf("tryStart: Received error from service.Start: %v", result.err)
			if isTerminalError(result.err) {
				return nil, result.err
			}
			c.updateState(supervisor.Failed, result.err)
			return nil, result.err
		}
		c.runStart = time.Now()
		log.Printf("tryStart: Service started successfully at %v", c.runStart)
		c.updateState(supervisor.Running, nil)
		return result.ch, nil

	case <-time.After(c.config.StartTimeout):
		log.Printf("tryStart: Service start timed out after %v", c.config.StartTimeout)
		cancel()
		c.updateState(supervisor.Failed, "start timeout")
		return nil, errors.New("service start timed out")

	case <-c.ctx.Done():
		log.Printf("tryStart: Controller context done")
		c.updateState(supervisor.Exited, "controller exited")
		return nil, supervisor.ErrExit
	}
}

func (c *Controller) tryStop(ctx context.Context) error {
	log.Printf("tryStop: Attempting to stop service")
	if c.state.getCurrentStatus() == supervisor.Stopped || c.state.getCurrentStatus() == supervisor.Exited {
		log.Printf("tryStop: Service already stopped or exited")
		return nil
	}
	c.updateState(supervisor.Stopping, nil)

	resultCh := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(ctx, c.config.StopTimeout)
	defer cancel()
	go func() {
		log.Printf("tryStop: Calling service.Stop")
		err := c.service.Stop(stopCtx)
		select {
		case resultCh <- err:
			log.Printf("tryStop: service.Stop returned, err: %v", err)
		case <-stopCtx.Done():
			log.Printf("tryStop: stop context done before sending result")
		}
	}()

	select {
	case err := <-resultCh:
		log.Printf("tryStop: Received result from service.Stop: %v", err)
		c.updateState(supervisor.Stopped, err)
		return err
	case <-stopCtx.Done():
		log.Printf("tryStop: Service stop timed out after %v", c.config.StopTimeout)
		c.updateState(supervisor.Failed, fmt.Sprintf("stop timeout after %v", c.config.StopTimeout))
		return fmt.Errorf("service stop timed out after %v", c.config.StopTimeout)
	}
}

func (c *Controller) tryRetry(attempt int32) {
	log.Printf("tryRetry: Scheduling retry for attempt %d", attempt)
	if int(attempt) >= c.config.RetryPolicy.MaxAttempts && c.config.RetryPolicy.MaxAttempts != 0 {
		log.Printf("tryRetry: Max attempts reached (%d); not retrying", c.config.RetryPolicy.MaxAttempts)
		return
	}
	bf := backoff.NewCalculator(c.config.RetryPolicy)
	delay := bf.NextInterval()
	log.Printf("tryRetry: Computed delay: %v", delay)

	select {
	case <-time.After(delay):
		log.Printf("tryRetry: Delay elapsed; enqueuing controlStart op")
		select {
		case c.ops <- controlOp{kind: controlStart, attempt: attempt}:
			log.Printf("tryRetry: controlStart op enqueued")
		case <-c.ctx.Done():
			log.Printf("tryRetry: Controller context done while enqueuing controlStart")
		}
	case <-c.ctx.Done():
		log.Printf("tryRetry: Controller context done during delay")
	}
}

// State returns the current state of the service.
func (c *Controller) State() State {
	return c.state.publicState()
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	log.Printf("updateState: Updating state to %v with details: %v", status, details)
	c.state.updateState(status, details)
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}
