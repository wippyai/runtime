package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	//log.Printf("NewController: Creating new controller for service: %+v", service)
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
	log.Printf("Start: Entering Start, active: %v", c.active)
	if !c.active {
		c.bootController()
	}
	c.mu.Unlock()

	result := make(chan error, 1)
	select {
	case c.ops <- controlOp{kind: controlStart, result: result}:
		log.Printf("Start: Control operation 'controlStart' sent")
		select {
		case err := <-result:
			log.Printf("!!!!!!!!!!!!!!!!Start: Received result from control operation: %v", err)
			return err
		case <-c.ctx.Done():
			log.Printf("@@@@@@@@@@@@@@@@Start: Supervisor context is done: %v", c.ctx.Err())
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}
	case <-c.ctx.Done():
		log.Printf("@@@@@@@@@@@@@@@@@Start: Supervisor context is done: %v", c.ctx.Err())
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}

func (c *Controller) Stop() error {
	log.Printf("Stop: Entering Stop")
	if c.ctx == nil {
		log.Printf("Stop: Context is nil, nothing to stop")
		return nil
	}

	result := make(chan error, 1)
	select {
	case c.ops <- controlOp{kind: controlStop, result: result}:
		log.Printf("Stop: Control operation 'controlStop' sent")
		select {
		case err := <-result:
			log.Printf("Stop: Received result from control operation: %v", err)
			if err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("failed to stop service: %w", err)
			}
		case <-c.root.Done():
			log.Printf("Stop: Supervisor context is done: %v", c.ctx.Err())
			return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
		}

	case <-c.root.Done():
		log.Printf("Stop: Supervisor context is already done")
		// Already stopping, just wait for cleanup
	}

	log.Printf("Stop: Exiting Stop")

	select {
	case <-c.ctx.Done():
		log.Printf("Stop: Supervisor context is done: %v", c.ctx.Err())
		return nil
	case <-c.root.Done():
		log.Printf("Stop: Supervisor context is done: %v", c.ctx.Err())
		return fmt.Errorf("supervisor is stopped: %w", c.ctx.Err())
	}
}

func (c *Controller) supervise() {
	defer func() {
		log.Printf("DEBUG: Supervision loop exiting - final status: %v", c.state.getCurrentStatus())
	}()

	log.Printf("supervise: Starting supervision loop for service: %+v", c.service)

	var startCh chan<- error

	for {
		log.Printf("supervise: Waiting for control operation or service details for service: %+v", c.service)
		select {
		case <-c.ctx.Done():
			log.Printf("supervise: Context cancelled, stopping service: %+v", c.service)
			if c.cancel != nil {
				_ = c.tryStop()
			}
			c.shutdown()
			return

		case op := <-c.ops:
			log.Printf("supervise: Received control operation: %+v", op)
			var err error
			switch op.kind {
			case controlStart:
				log.Printf("supervise: !!!!!!!!!!!!!!!!!Handling start for service: %+v", c.service)

				c.state.setDesiredStatus(supervisor.Running)
				c.bootContext()

				log.Printf("TGARTINGGGGG")
				detailsCh, sErr := c.tryStart()

				if sErr != nil {
					log.Printf("M<AIN COCOC ERRR %v", sErr)

					if isTerminalError(err) {
						c.updateState(supervisor.Exited, sErr)
						if startCh != nil {
							log.Printf("M<AIN COCOC ERRR TERM")
							// report to original caller
							select {
							case startCh <- err:
								log.Printf("WROTE @##, %v", err)
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
							log.Printf("M<AIN COCOC ERRR")
							// report to original caller
							select {
							case startCh <- err:
							case <-c.root.Done():
							}
							startCh = nil
						}
						log.Printf("M<AIN COCOC ERRR MAX")
						break
					}

					// Schedule retry
					log.Printf("RETRY")
					go c.tryRetry(int(attempt))
					if startCh == nil {
						// respond when we ready
						startCh = op.result
						op.result = nil
					}
					log.Printf("RETRY %v", err)
					break
				}

				go c.monitor(detailsCh)

				if startCh != nil {
					log.Printf("M<AIN COCOC III")
					// report to original caller
					select {
					case startCh <- nil:
						log.Printf("WROTE @##")
					case <-c.root.Done():
					}
					startCh = nil
				}

			case controlStop:
				c.state.setDesiredStatus(supervisor.Stopped)
				err = c.tryStop()

				if startCh != nil {
					log.Printf("M<AIN COCOC STT")
					// report to original caller
					select {
					case startCh <- context.Canceled:
						log.Printf("WROTE @##")
					case <-c.root.Done():
					}
					startCh = nil
				}

				if c.cancel != nil {
					c.cancel()
					c.cancel = nil
				}

			case controlExit:
				if startCh != nil {
					log.Printf("M<AIN COCOC УЧЧ")
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
					log.Printf("supervise: Sent result for control operation: %v", err)
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
	log.Printf("tryStart: Handling start for service: %+v", c.service)
	if c.state.getCurrentStatus() == supervisor.Running {
		log.Printf("tryStart: Service: %+v is already running", c.service)
		return nil, nil
	}

	// Reset retry count if this is a fresh start
	if c.state.getCurrentStatus() != supervisor.Failed {
		log.Printf("tryStart: Resetting retry count for service: %+v", c.service)
		c.state.resetRetryCount()
	}

	c.updateState(supervisor.Starting, fmt.Sprintf("attempt %d", c.state.getRetryCount()))

	// Start the service in a goroutine
	resultCh := make(chan struct {
		ch  <-chan any
		err error
	}, 1)

	go func() {
		log.Printf("tryStart: Starting service: %+v in goroutine", c.service)
		ch, err := c.service.Start(c.state.getContext())
		log.Printf("tryStart: Service: %+v start attempt finished, result: %v, err: %v", c.service, ch, err)
		select {
		case resultCh <- struct {
			ch  <-chan any
			err error
		}{ch, err}:
			log.Printf("tryStart: Sent service start result to resultCh")
		case <-c.state.getContext().Done():
			log.Printf("tryStart: Service: %+v start context cancelled", c.service)
		}
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			log.Printf("tryStart: Service: %+v failed to start: %v", c.service, result.err)
			if isTerminalError(result.err) {
				return nil, result.err
			}
			// Just update state and return error, let supervise handle retry
			c.updateState(supervisor.Failed, result.err)
			return nil, result.err
		}

		c.updateState(supervisor.Running, nil)
		log.Printf("tryStart: Service: %+v started successfully, details channel: %v", c.service, result.ch)
		return result.ch, nil

	case <-time.After(c.config.StartTimeout):
		c.state.cancelContext()
		log.Printf("tryStart: Service: %+v start timed out after %v", c.service, c.config.StartTimeout)
		c.updateState(supervisor.Failed, "start timeout")
		return nil, context.DeadlineExceeded

	case <-c.ctx.Done():
		log.Printf("tryStart: Service: %+v supervisor context cancelled", c.service)
		c.updateState(supervisor.Exited, "controller exited")
		return nil, supervisor.ExitErr
	}
}

func (c *Controller) tryStop() error {
	log.Printf("DEBUG: Starting stop sequence - current status: %v", c.state.getCurrentStatus())

	log.Printf("tryStop: Handling stop for service: %+v", c.service)
	if c.state.getCurrentStatus() == supervisor.Stopped {
		log.Printf("tryStop: Service: %+v is already stopped", c.service)
		return nil
	}

	c.updateState(supervisor.Stopping, nil)

	resultCh := make(chan error, 1)
	stopCtx, cancel := context.WithTimeout(c.root, c.config.StopTimeout)
	defer cancel()

	go func() {
		log.Printf("DEBUG: Stop goroutine started")
		log.Printf("!!!!tryStop: Stopping service: %+v in goroutine", c.service)
		err := c.service.Stop(stopCtx)
		log.Printf("tryStop: Service: %+v stop attempt finished, err: %v", c.service, err)
		log.Printf("DEBUG: Stop goroutine completed with err: %v", err)
		select {
		case resultCh <- err:
			log.Printf("tryStop: Sent service stop result to resultCh")
		case <-stopCtx.Done():
			log.Printf("tryStop: Service: %+v stop context cancelled", c.service)
		}
	}()
	log.Printf("DEBUG: Waiting for stop result")
	select {
	case err := <-resultCh:
		log.Printf("tryStop: Service: %+v stopped, err: %v", c.service, err)
		c.updateState(supervisor.Stopped, err)
		return err
	case <-stopCtx.Done():
		log.Printf("tryStop: Service: %+v stop timed out after %v", c.service, c.config.StopTimeout)
		c.updateState(supervisor.Failed, fmt.Sprintf("stop timeout after %v", c.config.StopTimeout))
		return fmt.Errorf("service stop timed out after %v", c.config.StopTimeout)
	}
}

func (c *Controller) tryRetry(attempt int) {
	log.Printf("tryRetry: Scheduling retry for service: %+v, attempt: %d", c.service, attempt)
	if attempt >= c.config.RetryPolicy.MaxAttempts {
		log.Printf("tryRetry: Max retry attempts reached for service: %+v", c.service)
		return
	}

	bf := backoff.NewCalculator(c.config.RetryPolicy)
	delay := bf.NextInterval()
	log.Printf("tryRetry: Retry for service: %+v scheduled in %v", c.service, delay)

	svcCtx := c.state.getContext()

	select {
	case <-time.After(delay):
		log.Printf("tryRetry: Attempting retry for service: %+v, attempt: %d", c.service, attempt)
		select {
		case c.ops <- controlOp{
			kind:    controlStart,
			attempt: attempt,
		}:
			log.Printf("tryRetry: Sent control operation 'controlRetry' for service: %+v, attempt: %d", c.service, attempt)
		case <-svcCtx.Done():
			log.Printf("tryRetry: Retry for service: %+v cancelled due to context done", c.service)
		}
	case <-svcCtx.Done():
		log.Printf("tryRetry: Retry for service: %+v cancelled due to context done", c.service)
	}
}

func (c *Controller) bootController() {
	log.Printf("bootController: Initializing controller for service: %+v", c.service)
	c.ops = make(chan controlOp, 10)
	go c.supervise()
	c.active = true

	c.ctx, c.cancel = context.WithCancel(c.root)
}

func (c *Controller) bootContext() {
	log.Printf("bootContext: Initializing service context for service: %+v", c.service)
	ctx, cancel := context.WithCancel(c.ctx)
	c.state.setContext(ctx, cancel)
}

func (c *Controller) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	log.Printf("shutdown: Shutting down controller for service: %+v", c.service)
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	close(c.ops)
	c.active = false
}

func (c *Controller) State() State {
	return c.state.publicState()
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	log.Printf("updateState: Updating state for service: %+v, status: %s, details: %v", c.service, status, details)
	c.state.updateState(status, details)
	log.Printf("updateState: Desired status for service: %+v changed to: %s", c.service, c.state.getDesiredStatus())
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}
