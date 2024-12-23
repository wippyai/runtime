package supervisor

import (
	"context"
	"errors"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/backoff"
	"sync"
	"time"
)

type State struct {
	Status     supervisor.Status
	Details    payload.Payload
	Desired    supervisor.Status
	RetryCount int32
	LastUpdate time.Time
}

type stateTransition struct {
	target supervisor.Status
	result chan error
}

// Controller manages the lifecycle of a service
type Controller struct {
	service       supervisor.Service
	config        supervisor.ServiceConfig
	state         *internalState
	transitions   chan stateTransition
	onStateChange func(supervisor.Status, payload.Payload)
	rootCtx       context.Context
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	supervising   bool
	mu            sync.Mutex
}

func NewController(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.ServiceConfig,
	onStateChange func(status supervisor.Status, details payload.Payload),
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

func (s *Controller) updateConfig(config supervisor.ServiceConfig) error {
	return nil
}

func (s *Controller) Start() error {
	s.mu.Lock()
	if !s.supervising {
		// init supervisor
		s.ctx, s.cancel = context.WithCancel(s.rootCtx)
		s.transitions = make(chan stateTransition, 1)
		s.wg.Add(1)
		go s.supervise()
		s.supervising = true
	}
	s.mu.Unlock()

	return s.transitionTo(supervisor.Running)
}

func (s *Controller) Stop() error {
	err := s.transitionTo(supervisor.Stopped)
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to transition to stopped: %w", err)
	}

	s.mu.Lock()
	if s.supervising {
		if s.cancel != nil {
			s.cancel()
		}

		// Wait for supervise goroutine to finish
		s.wg.Wait()

		// Clean up
		close(s.transitions)
		s.supervising = false
		s.ctx = nil
		s.cancel = nil
	}
	s.mu.Unlock()

	return nil
}

// transitionTo requests a state transition
func (s *Controller) transitionTo(status supervisor.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ctx == nil {
		return fmt.Errorf("supervisor is not started")
	}

	// Check context first
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("supervisor is stopped: %w", err)
	}

	result := make(chan error, 1)

	// Use separate select for sending transition
	select {
	case s.transitions <- stateTransition{target: status, result: result}:
		// Wait for result with context
		select {
		case err := <-result:
			return err
		case <-s.ctx.Done():
			return fmt.Errorf("supervisor is stopped: %w", s.ctx.Err())
		}
	case <-s.ctx.Done():
		return fmt.Errorf("supervisor is stopped: %w", s.ctx.Err())
	}
}

func (s *Controller) State() State {
	return s.state.publicState()
}

func (s *Controller) supervise() {
	defer s.wg.Done()

	for {
		select {
		case transition, ok := <-s.transitions:
			if !ok {
				return
			}
			err := s.handleTransition(transition.target)
			select {
			case transition.result <- err:
			default:
			}
		case <-s.ctx.Done():
			_ = s.tryStop()
			return
		}
	}
}

func (s *Controller) handleTransition(desired supervisor.Status) error {
	if !s.state.setDesiredStatus(desired) {
		return nil
	}

	switch desired {
	case supervisor.Running:
		if s.state.getCurrentStatus() != supervisor.Running {
			return s.startService()
		}
	case supervisor.Stopped:
		if s.state.getCurrentStatus() != supervisor.Stopped {
			return s.tryStop()
		}
	}

	return nil
}

func (s *Controller) startService() error {
	ctx, cancel := context.WithCancel(s.ctx)
	s.state.setContext(ctx, cancel)

	if err := s.tryStart(ctx, nil); err != nil {
		s.updateState(supervisor.Failed, payload.NewError(err))
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (s *Controller) recoverService(initialErr error) {
	if err := s.tryStart(s.ctx, initialErr); err != nil {
		s.updateState(supervisor.Failed, payload.NewError(err))
	}
}

func (s *Controller) tryStart(ctx context.Context, lastErr error) error {
	bf := backoff.NewCalculator(s.config.RetryPolicy)

	for {
		s.updateState(
			supervisor.Starting,
			payload.NewString(fmt.Sprintf("Attempt %d", s.state.getSnapshot().retryCount)),
		)

		startCtx, cancel := context.WithTimeout(ctx, s.config.StartTimeout)
		detailsCh, err := s.service.Start(startCtx)

		// Check context cancellation immediately after service.Start
		if startCtx.Err() != nil {
			cancel()
			return fmt.Errorf("service start timeout: %w", startCtx.Err())
		}
		cancel()

		if err == nil {
			s.wg.Add(1)
			s.updateState(supervisor.Running, nil)
			go s.monitorService(detailsCh)
			return nil
		}
		lastErr = err

		// report state
		s.updateState(supervisor.Failed, payload.NewError(lastErr))

		if !s.state.canRecover(s.config.RetryPolicy.MaxAttempts, s.ctx) {
			return fmt.Errorf("failed to start service: %w", lastErr)
		}

		s.state.incRetryCount()

		interval := bf.NextInterval()
		if interval == 0 {
			s.updateState(supervisor.Failed, payload.NewError(lastErr))
			return fmt.Errorf("failed to start service: %w", lastErr)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			continue
		}
	}
}

func (s *Controller) tryStop() error {
	if s.state.getCurrentStatus() == supervisor.Stopped {
		return nil
	}

	s.updateState(supervisor.Stopping, nil)

	// Try graceful shutdown first with timeout
	shutdownCtx, cancel := context.WithTimeout(s.rootCtx, s.config.StopTimeout)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.service.Stop(shutdownCtx)
	}()

	// Wait for either completion or timeout
	var err error
	select {
	case err = <-errCh:
		// Service stopped normally
	case <-shutdownCtx.Done():
		// Timeout occurred
		err = fmt.Errorf("service stop timeout after %v", s.config.StopTimeout)
	}

	// Always update state to Stopped, regardless of error
	s.updateState(supervisor.Stopped, nil)

	// Return any error that occurred
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}

	return nil
}

func (s *Controller) monitorService(detailsCh <-chan payload.Payload) {
	defer s.wg.Done()

	ctx := s.state.getContext()
	for {
		select {
		case details, ok := <-detailsCh:
			if !ok {
				if s.state.getDesiredStatus() == supervisor.Running {
					s.handleError(fmt.Errorf("service Details channel closed unexpectedly"))
				}
				return
			}
			status, details := s.state.updateDetails(details)
			if s.onStateChange != nil {
				s.onStateChange(status, details)
			}
		case <-ctx.Done():
			if s.state.getDesiredStatus() == supervisor.Running {
				s.updateState(supervisor.Stopped, nil)
			}
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Controller) handleError(err error) {
	s.updateState(supervisor.Failed, payload.NewError(err))
	if s.state.canRecover(s.config.RetryPolicy.MaxAttempts, s.ctx) {
		go s.recoverService(err)
	}
}

func (s *Controller) updateState(status supervisor.Status, details payload.Payload) {
	s.state.updateState(status, details)
	if s.onStateChange != nil {
		s.onStateChange(status, details)
	}
}
