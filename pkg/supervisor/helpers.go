package supervisor

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"go.uber.org/zap"
	"sync"
	"time"
)

type internalState struct {
	mu         sync.Mutex
	status     supervisor.Status
	details    any
	desired    supervisor.Status
	retryCount int32
	lastUpdate time.Time
	ctx        context.Context
	cancel     context.CancelFunc
	mur        sync.Mutex
	runCtx     context.Context
	runCancel  context.CancelFunc
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

// setContext updates the context and cancel function
func (s *internalState) setContext(ctx context.Context, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.ctx = ctx
	s.cancel = cancel
}

// getContext returns the current context
func (s *internalState) getContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.ctx
}

// cancelContext cancels the current context if it exists
func (s *internalState) cancelContext() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
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

// canRecover checks if the service can be recovered based on current state
func (s *internalState) canRecover(maxAttempts int, ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return false
	}

	return s.desired == supervisor.Running && int(s.retryCount) < maxAttempts
}

// setDesiredStatus updates the desired state and returns if it changed
func (s *internalState) setDesiredStatus(desired supervisor.Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if desired == s.desired {
		return false
	}
	s.desired = desired

	return true
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
	register map[registry.ID]*supervisor.Entry
	remove   map[registry.ID]struct{}
	logger   *zap.Logger
}

func newTransactionHelper(logger *zap.Logger) *registryTX {
	return &registryTX{
		register: make(map[registry.ID]*supervisor.Entry),
		remove:   make(map[registry.ID]struct{}),
		logger:   logger,
	}
}

func (th *registryTX) begin() {
	if th.open {
		th.logger.Warn("received begin transaction while already in transaction, resetting state")
	}

	th.open = true
	th.register = make(map[registry.ID]*supervisor.Entry)
	th.remove = make(map[registry.ID]struct{})
}

func (th *registryTX) commit(removeFn func(registry.ID) error, registerFn func(registry.ID, *supervisor.Entry) error) error {
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

	th.reset()
	th.logger.Info("discarded all tx service changes")
}

func (th *registryTX) registerService(id registry.ID, entry *supervisor.Entry) error {
	if !th.open {
		return fmt.Errorf("received register action outside of transaction")
	}

	delete(th.remove, id)
	th.register[id] = entry
	return nil
}

func (th *registryTX) removeService(id registry.ID) error {
	if !th.open {
		return fmt.Errorf("received remove action outside of transaction")
	}

	delete(th.register, id)
	th.remove[id] = struct{}{}

	return nil
}

func (th *registryTX) reset() {
	th.open = false
	th.register = make(map[registry.ID]*supervisor.Entry)
	th.remove = make(map[registry.ID]struct{})
}
