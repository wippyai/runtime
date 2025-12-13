package supervisor

import (
	"context"
	"errors"
	"sync"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/supervisor"
)

// State represents the current state of a supervised service,
// including its status, desired status, and retry attempt information.
type State struct {
	Status     supervisor.Status `json:"status"`
	Details    any               `json:"details"`
	Desired    supervisor.Status `json:"desired"`
	RetryCount int32             `json:"retry_count"`
	LastUpdate time.Time         `json:"last_update"`
	StartedAt  time.Time         `json:"started_at"`
}

type internalState struct {
	mu         sync.Mutex
	status     supervisor.Status
	details    any
	desired    supervisor.Status
	retryCount int32
	lastUpdate time.Time
	startedAt  time.Time
}

// isTerminalError determines if the error represents a terminal state
// that should not be retried
func isTerminalError(err error) bool {
	if err == nil {
		return false
	}

	// Check sentinel errors first
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, supervisor.ErrTerminated) ||
		errors.Is(err, supervisor.ErrExit) {
		return true
	}

	// Check if error implements apierror.Error and is explicitly not retryable
	var apiErr apierror.Error
	if errors.As(err, &apiErr) {
		if apiErr.Retryable() == apierror.False {
			return true
		}
	}

	return false
}

// newInternalState creates a new internalState instance
func newInternalState() *internalState {
	return &internalState{
		status:     supervisor.StatusUnknown,
		desired:    supervisor.StatusUnknown,
		lastUpdate: time.Now(),
		startedAt:  time.Time{},
	}
}

// getSnapshot returns a copy of the current state
func (s *internalState) getSnapshot() internalState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return internalState{
		status:     s.status,
		details:    s.details,
		desired:    s.desired,
		retryCount: s.retryCount,
		lastUpdate: s.lastUpdate,
		startedAt:  s.startedAt,
	}
}

func (s *internalState) publicState() State {
	s.mu.Lock()
	defer s.mu.Unlock()

	return State{
		Status:     s.status,
		Details:    s.details,
		Desired:    s.desired,
		RetryCount: s.retryCount,
		LastUpdate: s.lastUpdate,
		StartedAt:  s.startedAt,
	}
}

// updateState updates the service state and returns current details
func (s *internalState) updateState(status supervisor.Status, details any) (supervisor.Status, any) {
	s.mu.Lock()
	prevStatus := s.status
	s.status = status
	if status == supervisor.StatusRunning && prevStatus != supervisor.StatusRunning {
		s.startedAt = time.Now()
	}
	s.mu.Unlock()

	return s.updateDetails(details)
}

// updateDetails updates only the details and returns current status
func (s *internalState) updateDetails(details any) (supervisor.Status, any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.details = details
	s.lastUpdate = time.Now()

	return s.status, details
}

// incRetryCount increases the retry count and returns the new value
func (s *internalState) incRetryCount() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.retryCount++
	return s.retryCount
}

// getRetryCount returns the current retry count
func (s *internalState) getRetryCount() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.retryCount
}

// resetRetryCount resets the retry counter to zero
func (s *internalState) resetRetryCount() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.retryCount = 0
}

// setDesiredStatus updates the desired state and returns if it changed
func (s *internalState) setDesiredStatus(desired supervisor.Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.desired = desired
}

// getCurrentStatus returns the current status
func (s *internalState) getCurrentStatus() supervisor.Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.status
}

// getDesiredStatus returns the desired status
func (s *internalState) getDesiredStatus() supervisor.Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.desired
}
