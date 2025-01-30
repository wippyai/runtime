package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
)

// State represents the current state of a supervised service,
// including its status, desired status, and retry attempt information.
type State struct {
	Status     supervisor.Status `json:"status"`
	Details    any               `json:"details"`
	Desired    supervisor.Status `json:"desired"`
	RetryCount int32             `json:"retry_count"`
	LastUpdate time.Time         `json:"last_update"`
}

type internalState struct {
	mu         sync.Mutex
	status     supervisor.Status
	details    any
	desired    supervisor.Status
	retryCount int32
	lastUpdate time.Time
}

// isTerminalError determines if the error represents a terminal state
// that should not be retried
func isTerminalError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, supervisor.ErrTerminated) ||
		errors.Is(err, supervisor.ErrExit)
}

// newServiceState creates a new internalState instance
func newServiceState() *internalState {
	return &internalState{
		status:     supervisor.Unknown,
		desired:    supervisor.Unknown,
		lastUpdate: time.Now(),
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
	}
}

// updateState updates the service state and returns current details
func (s *internalState) updateState(status supervisor.Status, details any) (supervisor.Status, any) {
	s.mu.Lock()
	s.status = status
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

type registryTX struct {
	open     bool
	register map[string]*supervisor.Entry
	remove   map[string]struct{}
	logger   *zap.Logger
}

func newTransactionHelper(logger *zap.Logger) *registryTX {
	return &registryTX{
		register: make(map[string]*supervisor.Entry),
		remove:   make(map[string]struct{}),
		logger:   logger,
	}
}

func (th *registryTX) begin() {
	if th.open {
		th.logger.Warn("received begin transaction while already in transaction, resetting state")
	}

	th.open = true
	th.register = make(map[string]*supervisor.Entry)
	th.remove = make(map[string]struct{})
}

func (th *registryTX) commit(removeFn func(string) error, registerFn func(string, *supervisor.Entry) error) error {
	if !th.open {
		th.logger.Warn("received commit without active transaction")
		return nil
	}

	// Apply all tx changes
	for id := range th.remove {
		if err := removeFn(id); err != nil {
			return fmt.Errorf("failed to remove service %s during commit: %w", id, err)
		}
	}

	for id, entry := range th.register {
		if err := registerFn(id, entry); err != nil {
			return fmt.Errorf("failed to register service %s during commit: %w", id, err)
		}
	}

	th.reset()
	return nil
}

func (th *registryTX) discard() {
	if !th.open {
		th.logger.Warn("received discard without active transaction")
		return
	}

	if len(th.register) > 0 || len(th.remove) > 0 {
		th.logger.Warn("discarding transaction with pending changes")
	}

	th.reset()
}

func (th *registryTX) registerService(id string, entry *supervisor.Entry) error {
	if !th.open {
		return fmt.Errorf("received register action outside of transaction")
	}

	if _, exists := th.register[id]; exists {
		return nil
	}

	delete(th.remove, id)
	th.register[id] = entry
	return nil
}

func (th *registryTX) removeService(id string) error {
	if !th.open {
		return fmt.Errorf("received remove action outside of transaction")
	}

	// duplicate check
	if _, exists := th.remove[id]; exists {
		return nil
	}

	delete(th.register, id)
	th.remove[id] = struct{}{}

	return nil
}

func (th *registryTX) reset() {
	th.open = false
	th.register = make(map[string]*supervisor.Entry)
	th.remove = make(map[string]struct{})
}
