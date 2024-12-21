package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/supervisor"
	"sync"
	"sync/atomic"
	"time"
)

// ServiceState represents the internal state of a service
type ServiceState struct {
	atomic.Value              // stores supervisor.ServiceState
	desired      atomic.Value // stores supervisor.Status
	retryCount   atomic.Int32
	lastUpdate   atomic.Value // stores time.Time of last status update
	ctx          context.Context
	cancel       context.CancelFunc
}

// Supervisor manages the lifecycle of a service
type Supervisor struct {
	service        supervisor.Service
	config         supervisor.ServiceConfig
	state          *ServiceState
	transitions    chan stateTransition
	onStatusChange func(supervisor.ServiceState)
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	stateMu        sync.RWMutex
}

type stateTransition struct {
	target supervisor.Status
	result chan error
}

func NewSupervisor(
	ctx context.Context,
	service supervisor.Service,
	config supervisor.ServiceConfig,
	onStatusChange func(supervisor.ServiceState),
) *Supervisor {
	ctx, cancel := context.WithCancel(ctx)

	state := &ServiceState{}
	state.Value.Store(supervisor.ServiceState{Status: supervisor.Unknown})
	state.desired.Store(supervisor.Unknown)
	state.lastUpdate.Store(time.Now())

	s := &Supervisor{
		service:        service,
		config:         config,
		state:          state,
		transitions:    make(chan stateTransition, 1),
		onStatusChange: onStatusChange,
		ctx:            ctx,
		cancel:         cancel,
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

// GetState returns the current service state
func (s *Supervisor) GetState() supervisor.ServiceState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state.Value.Load().(supervisor.ServiceState)
}

// GetLastUpdateTime returns the timestamp of the last status update
func (s *Supervisor) GetLastUpdateTime() time.Time {
	return s.state.lastUpdate.Load().(time.Time)
}

// Stop gracefully stops the supervisor and underlying service
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
			_ = s.stopService()
			return
		}
	}
}

func (s *Supervisor) handleTransition(desired supervisor.Status) error {
	current := s.state.desired.Load().(supervisor.Status)
	if desired == current {
		return nil
	}

	s.state.desired.Store(desired)

	switch desired {
	case supervisor.Running:
		if s.GetState().Status != supervisor.Running {
			return s.startService()
		}
	case supervisor.Stopped:
		if s.GetState().Status != supervisor.Stopped {
			return s.stopService()
		}
	}

	return nil
}

func (s *Supervisor) startService() error {
	ctx, cancel := context.WithCancel(s.ctx)
	s.state.cancel = cancel
	s.state.ctx = ctx

	if err := s.startWithBackoff(ctx, nil); err != nil {
		s.safeUpdateStatus(supervisor.ServiceState{
			Status:  supervisor.Failed,
			Details: payload.NewError(err),
		})
		return fmt.Errorf("failed to start service: %w", err)
	}

	return nil
}

func (s *Supervisor) recoverService(initialErr error) {
	s.state.retryCount.Add(1)

	recoveryCtx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	if err := s.startWithBackoff(recoveryCtx, initialErr); err != nil {
		s.safeUpdateStatus(supervisor.ServiceState{
			Status:  supervisor.Failed,
			Details: payload.NewError(err),
		})
	}
}

func (s *Supervisor) startWithBackoff(ctx context.Context, initialErr error) error {
	backoff := NewBackoffCalculator(s.config.RetryPolicy)

	for {
		s.safeUpdateStatus(supervisor.ServiceState{
			Status:  supervisor.Starting,
			Details: payload.NewString(fmt.Sprintf("Attempt %d", s.state.retryCount.Load())),
		})

		if !s.canRecover() {
			if initialErr != nil {
				return initialErr
			}
			return fmt.Errorf("service recovery failed after %d attempts", s.state.retryCount.Load())
		}

		startCtx, cancel := context.WithTimeout(ctx, s.config.StartTimeout)
		statusCh, err := s.service.Start(startCtx)
		cancel()

		if err == nil {
			s.wg.Add(1)
			go s.monitorService(statusCh)
			return nil
		}

		s.state.retryCount.Add(1)

		interval := backoff.NextInterval()
		if interval == 0 {
			return fmt.Errorf("service recovery failed after %d attempts", s.state.retryCount.Load())
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			continue
		}
	}
}

func (s *Supervisor) canRecover() bool {
	desired := s.state.desired.Load().(supervisor.Status)
	retries := s.state.retryCount.Load()

	if s.ctx.Err() != nil {
		return false
	}

	return desired == supervisor.Running && int(retries) < s.config.RetryPolicy.MaxAttempts
}

func (s *Supervisor) monitorService(statusCh <-chan supervisor.ServiceState) {
	defer s.wg.Done()

	for {
		select {
		case status, ok := <-statusCh:
			if !ok {
				if s.state.desired.Load().(supervisor.Status) == supervisor.Running {
					s.handleStatus(s.endingState())
				}
				return
			}
			s.handleStatus(status)
		case <-s.state.ctx.Done():
			if s.state.desired.Load().(supervisor.Status) == supervisor.Running {
				s.handleStatus(supervisor.ServiceState{Status: supervisor.Stopped})
			}
			return
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Supervisor) endingState() supervisor.ServiceState {
	if s.state.desired.Load().(supervisor.Status) == supervisor.Running {
		return supervisor.ServiceState{
			Status:  supervisor.Failed,
			Details: payload.NewError(fmt.Errorf("service status channel closed unexpectedly")),
		}
	}
	return supervisor.ServiceState{Status: supervisor.Stopped}
}

func (s *Supervisor) stopService() error {
	currentState := s.GetState()
	if currentState.Status == supervisor.Stopped {
		return nil
	}

	s.safeUpdateStatus(supervisor.ServiceState{Status: supervisor.Stopping})

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.StopTimeout)
	defer cancel()

	if err := s.service.Stop(shutdownCtx); err != nil {
		if s.config.ForceShutdown {
			if s.state.cancel != nil {
				s.state.cancel()
			}
			s.safeUpdateStatus(supervisor.ServiceState{
				Status:  supervisor.Stopped,
				Details: payload.NewError(fmt.Errorf("forced shutdown due to error: %w", err)),
			})
			return nil
		}
		return fmt.Errorf("failed to stop service: %w", err)
	}

	s.safeUpdateStatus(supervisor.ServiceState{Status: supervisor.Stopped})
	return nil
}

func (s *Supervisor) handleStatus(status supervisor.ServiceState) {
	switch status.Status {
	case supervisor.Running:
		s.state.retryCount.Store(0)
		s.safeUpdateStatus(status)
	case supervisor.Failed:
		s.safeUpdateStatus(status)
		if s.canRecover() {
			go s.recoverService(s.extractError(status))
		}
	default:
		s.safeUpdateStatus(status)
	}
}

func (s *Supervisor) extractError(status supervisor.ServiceState) error {
	if status.Details != nil && status.Details.Format() == payload.Error {
		if err, ok := status.Details.Data().(error); ok {
			return err
		}
	}
	return nil
}

// safeUpdateStatus updates the service state with duplicate prevention
func (s *Supervisor) safeUpdateStatus(newState supervisor.ServiceState) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	currentState := s.state.Value.Load().(supervisor.ServiceState)

	// Always allow Running status updates
	// For other statuses, prevent duplicates
	if currentState.Status == newState.Status && newState.Status != supervisor.Running {
		return
	}

	s.state.Value.Store(newState)
	s.state.lastUpdate.Store(time.Now())

	if s.onStatusChange != nil {
		s.onStatusChange(newState)
	}
}
