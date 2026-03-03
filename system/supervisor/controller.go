// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	securitysys "github.com/wippyai/runtime/system/security"

	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/internal/backoff"
)

type ctrlKind int

const (
	ctrlStart ctrlKind = iota
	ctrlStop
	ctrlFailed
	ctrlExit
)

// controllable defines the interface for service lifecycle control operations.
type controllable interface {
	Start() error
	Stop() error
}

type ctrlOp struct {
	result  chan error
	kind    ctrlKind
	attempt int32
}

// Controller manages the lifecycle of a service.
type Controller struct {
	runStart      time.Time
	service       supervisor.Service
	root          context.Context
	ctx           context.Context
	state         *internalState
	onStateChange func(supervisor.Status, any)
	cancel        context.CancelFunc
	ops           chan ctrlOp
	config        supervisor.LifecycleConfig
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
		state:         newInternalState(),
		onStateChange: onStateChange,
		root:          ctx,
		ops:           make(chan ctrlOp, 10),
	}

	// Create isolated FrameContext for this service lifecycle.
	ctx, fc := ctxapi.OpenFrameContextOn(ctx, ctx)

	if config.Security != nil {
		ctx = securitysys.WithSecurityConfig(ctx, config.Security)
	}

	// Seal the frame since this is service-level and won't be modified
	fc.Seal()

	ctrl.ctx, ctrl.cancel = context.WithCancel(ctx)

	go func() {
		defer ctxapi.ReleaseFrameContext(fc)
		ctrl.supervise()
	}()
	return ctrl
}

// Start initiates the service and transitions it to the running state.
func (c *Controller) Start() error {
	c.state.setDesiredStatus(supervisor.StatusRunning)
	return c.runCommand(ctrlOp{kind: ctrlStart})
}

// Stop gracefully stops the service and transitions it to the stopped state.
func (c *Controller) Stop() error {
	c.state.setDesiredStatus(supervisor.StatusStopped)
	return c.runCommand(ctrlOp{kind: ctrlStop})
}

func (c *Controller) runCommand(op ctrlOp) error {
	op.result = make(chan error, 1)
	select {
	case c.ops <- op:
		select {
		case err := <-op.result:
			if err != nil && !errors.Is(err, context.Canceled) {
				return NewStopError(err)
			}
			return err
		case <-c.ctx.Done():
			return NewSupervisorStoppedError(c.ctx.Err())
		}
	case <-c.ctx.Done():
		return NewSupervisorStoppedError(c.ctx.Err())
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
			return

		case op := <-c.ops:
			var err error
			switch op.kind {
			case ctrlStop:
				if ctx == nil {
					break
				}
				err = c.tryStop(ctx)
				if cancel != nil {
					cancel()
					cancel = nil
				}
				if exitCh != nil {
					select {
					case <-exitCh:
					case <-c.ctx.Done():
					}
					exitCh = nil
				}
				respondAndCancel(context.Canceled)

			case ctrlExit:
				c.updateState(supervisor.StatusExited, nil)
				respondAndCancel(context.Canceled)
				if cancel != nil {
					cancel()
					cancel = nil
				}

			case ctrlFailed:
				attempt := c.state.incRetryCount()
				c.updateState(supervisor.StatusFailed, c.state.details)
				if c.state.getDesiredStatus() == supervisor.StatusRunning {
					if !c.runStart.IsZero() && time.Since(c.runStart) >= c.config.StableThreshold {
						c.state.resetRetryCount()
						attempt = 1
					}
					c.runStart = time.Time{}
					if c.config.RetryPolicy.MaxAttempts == 0 || int(attempt) < c.config.RetryPolicy.MaxAttempts {
						go c.tryRetry(attempt)
					}
				}
				continue

			case ctrlStart:
				if c.state.getCurrentStatus() == supervisor.StatusRunning {
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
						c.updateState(supervisor.StatusExited, sErr)
						respondAndCancel(sErr)
						break
					}
					if c.state.getDesiredStatus() != supervisor.StatusRunning {
						respondStart(context.Canceled)
						break
					}
					attempt := c.state.incRetryCount()
					if c.config.RetryPolicy.MaxAttempts != 0 && int(attempt) >= c.config.RetryPolicy.MaxAttempts {
						err = context.DeadlineExceeded
						respondStart(err)
						break
					}
					go c.tryRetry(attempt)
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
				if c.state.getDesiredStatus() == supervisor.StatusRunning {
					select {
					case c.ops <- ctrlOp{kind: ctrlFailed}:
					case <-ctx.Done():
					}
				}
				return
			}
			if err, isErr := details.(error); isErr && isTerminalError(err) {
				select {
				case c.ops <- ctrlOp{kind: ctrlExit}:
				case <-ctx.Done():
				}
				return
			}
			status, updatedDetails := c.state.updateDetails(details)
			if c.onStateChange != nil {
				c.onStateChange(status, updatedDetails)
			}
		}
	}
}

func (c *Controller) tryStart(ctx context.Context, cancel context.CancelFunc) (<-chan any, error) {
	c.updateState(
		supervisor.StatusStarting,
		fmt.Sprintf("attempt %d", c.state.getRetryCount()+1),
	)
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
			c.updateState(supervisor.StatusFailed, result.err)
			return nil, result.err
		}
		c.runStart = time.Now()
		c.updateState(supervisor.StatusRunning, nil)
		return result.ch, nil
	case <-time.After(c.config.StartTimeout):
		cancel()
		c.updateState(supervisor.StatusFailed, "start timeout")
		return nil, ErrStartTimeout
	case <-c.ctx.Done():
		c.updateState(supervisor.StatusExited, "controller exited")
		return nil, supervisor.ErrExit
	}
}

func (c *Controller) tryStop(ctx context.Context) error {
	status := c.state.getCurrentStatus()
	if status == supervisor.StatusStopped || status == supervisor.StatusExited {
		return nil
	}
	c.updateState(supervisor.StatusStopping, nil)
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
		c.updateState(supervisor.StatusStopped, err)
		return err
	case <-stopCtx.Done():
		c.updateState(supervisor.StatusFailed, "stop timeout after "+c.config.StopTimeout.String())
		return NewStopTimeoutError(c.config.StopTimeout)
	}
}

func (c *Controller) tryRetry(attempt int32) {
	if int(attempt) >= c.config.RetryPolicy.MaxAttempts && c.config.RetryPolicy.MaxAttempts != 0 {
		return
	}
	bf := backoff.NewCalculator(c.config.RetryPolicy)
	delay := bf.NextInterval()
	select {
	case <-time.After(delay):
		select {
		case c.ops <- ctrlOp{kind: ctrlStart, attempt: attempt}:
		case <-c.ctx.Done():
		}
	case <-c.ctx.Done():
	}
}

// State returns the current public state of the service, including status,
// details, desired state, retry count, and last update time.
func (c *Controller) State() State {
	return c.state.publicState()
}

func (c *Controller) updateState(status supervisor.Status, details any) {
	c.state.updateState(status, details)
	if c.onStateChange != nil {
		c.onStateChange(status, details)
	}
}
