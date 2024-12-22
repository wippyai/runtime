package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/internal/backoff"
	"sync"
	"time"
)

type serviceState struct {
	mu         sync.Mutex
	status     supervisor.Status
	details    payload.Payload
	desired    supervisor.Status
	retryCount int32
	lastUpdate time.Time
	ctx        context.Context
	cancel     context.CancelFunc
}

// newServiceState creates a new serviceState instance
func newServiceState() *serviceState {
	return &serviceState{
		status:     supervisor.Unknown,
		desired:    supervisor.Unknown,
		lastUpdate: time.Now(),
	}
}

// getSnapshot returns a copy of the current state
func (s *serviceState) getSnapshot() serviceState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return serviceState{
		status:     s.status,
		details:    s.details,
		desired:    s.desired,
		retryCount: s.retryCount,
		lastUpdate: s.lastUpdate,
	}
}

// setContext updates the context and cancel function
func (s *serviceState) setContext(ctx context.Context, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctx = ctx
	s.cancel = cancel
}

// getContext returns the current context
func (s *serviceState) getContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.ctx
}

// cancelContext cancels the current context if it exists
func (s *serviceState) cancelContext() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
}

// updateState updates the service state and returns current details
func (s *serviceState) updateState(status supervisor.Status, details payload.Payload) (supervisor.Status, payload.Payload) {
	s.mu.Lock()
	s.status = status
	s.mu.Unlock()

	return s.updateDetails(details)
}

// updateDetails updates only the details and returns current status
func (s *serviceState) updateDetails(details payload.Payload) (supervisor.Status, payload.Payload) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.details = details
	s.lastUpdate = time.Now()

	return s.status, details
}

// incRetryCount increases the retry count and returns the new value
func (s *serviceState) incRetryCount() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.retryCount++
	return s.retryCount
}

// canRecover checks if the service can be recovered based on current state
func (s *serviceState) canRecover(maxAttempts int, ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return false
	}

	return s.desired == supervisor.Running && int(s.retryCount) < maxAttempts
}

// setDesiredStatus updates the desired state and returns if it changed
func (s *serviceState) setDesiredStatus(desired supervisor.Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if desired == s.desired {
		return false
	}
	s.desired = desired

	return true
}

// getCurrentStatus returns the current status
func (s *serviceState) getCurrentStatus() supervisor.Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

// getDesiredStatus returns the desired status
func (s *serviceState) getDesiredStatus() supervisor.Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.desired
}

type stateTransition struct {
	target supervisor.Status
	result chan error
}

// Supervisor manages the lifecycle of a service
type Supervisor struct {
	service       supervisor.Service
	config        supervisor.ServiceConfig
	state         *serviceState
	transitions   chan stateTransition
	onStateChange func(supervisor.Status, payload.Payload)
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
}

func NewSupervisor(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.ServiceConfig,
	onStateChange func(status supervisor.Status, details payload.Payload),
) *Supervisor {
	ctx, cancel := context.WithCancel(ctx)

	s := &Supervisor{
		service:       service,
		config:        config,
		state:         newServiceState(),
		transitions:   make(chan stateTransition, 1),
		onStateChange: onStateChange,
		ctx:           ctx,
		cancel:        cancel,
	}

	s.wg.Add(1)
	go s.supervise()

	return s
}

// TransitionTo requests a state transition
func (s *Supervisor) TransitionTo(status supervisor.Status) error {
	result := make(chan error, 1)
	select {
	case s.transitions <- stateTransition{target: status, result: result}:
		return <-result
	case <-s.ctx.Done():
		return fmt.Errorf("supervisor is stopped: %w", s.ctx.Err())
	}
}

func (s *Supervisor) Start() error {
	err := s.TransitionTo(supervisor.Running)
	if err != nil {
		return fmt.Errorf("failed to transition to running: %w", err)
	}
	return nil
}

func (s *Supervisor) Stop() error {
	err := s.TransitionTo(supervisor.Stopped)
	if err != nil {
		return fmt.Errorf("failed to transition to stopped: %w", err)
	}

	s.cancel()
	s.wg.Wait()
	close(s.transitions)
	return nil
}

func (s *Supervisor) supervise() {
	defer s.wg.Done()

	for {
		select {
		case transition := <-s.transitions:
			err := s.handleTransition(transition.target)
			transition.result <- err
		case <-s.ctx.Done():
			_ = s.tryStop()
			return
		}
	}
}

func (s *Supervisor) handleTransition(desired supervisor.Status) error {
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

func (s *Supervisor) startService() error {
	ctx, cancel := context.WithCancel(s.ctx)
	s.state.setContext(ctx, cancel)

	if err := s.tryStart(ctx, nil); err != nil {
		s.updateState(supervisor.Failed, payload.NewError(err))
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (s *Supervisor) recoverService(initialErr error) {
	if err := s.tryStart(s.ctx, initialErr); err != nil {
		s.updateState(supervisor.Failed, payload.NewError(err))
	}
}

func (s *Supervisor) tryStart(ctx context.Context, lastErr error) error {
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

		if !s.state.canRecover(s.config.RetryPolicy.MaxAttempts, s.ctx) {
			s.updateState(supervisor.Failed, payload.NewError(lastErr))
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

func (s *Supervisor) tryStop() error {
	if s.state.getCurrentStatus() == supervisor.Stopped {
		return nil
	}

	s.updateState(supervisor.Stopping, nil)
	s.state.cancelContext()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.StopTimeout)
	defer cancel()

	if err := s.service.Stop(shutdownCtx); err != nil {
		if s.config.ForceShutdown {
			s.state.cancelContext()
			s.updateState(
				supervisor.Stopped,
				payload.NewError(fmt.Errorf("forced shutdown due to error: %w", err)),
			)
			return nil
		}

		return fmt.Errorf("failed to stop service: %w", err)
	}

	s.updateState(supervisor.Stopped, nil)
	return nil
}

func (s *Supervisor) monitorService(detailsCh <-chan payload.Payload) {
	defer s.wg.Done()

	ctx := s.state.getContext()
	for {
		select {
		case details, ok := <-detailsCh:
			if !ok {
				if s.state.getDesiredStatus() == supervisor.Running {
					s.handleError(fmt.Errorf("service details channel closed unexpectedly"))
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

func (s *Supervisor) handleError(err error) {
	s.updateState(supervisor.Failed, payload.NewError(err))
	if s.state.canRecover(s.config.RetryPolicy.MaxAttempts, s.ctx) {
		go s.recoverService(err)
	}
}

func (s *Supervisor) updateState(status supervisor.Status, details payload.Payload) {
	s.state.updateState(status, details)
	if s.onStateChange != nil {
		s.onStateChange(status, details)
	}
}
