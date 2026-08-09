// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

var (
	ErrSourceClosed = errors.New("cdc source slot is closed")
	ErrSourceBusy   = errors.New("cdc source slot is stopping")
)

type slotState uint8

const (
	slotIdle slotState = iota
	slotStarting
	slotRunning
	slotStopping
	slotFaulted
	slotStopped
)

// sourceSlot is the stable object placed in both the system registry and the
// supervisor. A driver replacement changes the delegated generation, never the
// supervisor object or registry pointer.
type sourceSlot struct {
	id   registry.ID
	kind registry.Kind

	opMu sync.Mutex
	mu   sync.RWMutex

	current    ManagedSource
	log        *zap.Logger
	generation uint64
	state      slotState
	runCtx     context.Context
	runCancel  context.CancelFunc
	status     chan any
	statusDone bool
	replacing  bool
	disposing  bool
}

func newSourceSlot(id registry.ID, kind registry.Kind, source ManagedSource, logs ...*zap.Logger) *sourceSlot {
	log := zap.NewNop()
	if len(logs) > 0 && logs[0] != nil {
		log = logs[0]
	}
	return &sourceSlot{
		id:         canonicalID(id),
		kind:       kind,
		current:    source,
		log:        log,
		generation: 1,
		state:      slotIdle,
	}
}

func (s *sourceSlot) Info() api.SourceInfo {
	s.mu.RLock()
	current := s.current
	generation := s.generation
	state := s.state
	s.mu.RUnlock()
	if current == nil {
		return api.SourceInfo{
			ID:         s.id,
			Kind:       s.kind,
			Generation: generationString(generation),
			State:      sourceState(state),
		}
	}
	info := current.Info()
	info.ID = s.id
	info.Kind = s.kind
	info.Generation = generationString(generation)
	info.State = sourceState(state)
	return info
}

func (s *sourceSlot) Subscribe(ctx context.Context, opts api.StreamOptions) (api.Stream, error) {
	s.mu.RLock()
	if s.state != slotRunning || s.current == nil || s.disposing {
		s.mu.RUnlock()
		return nil, api.ErrSourceNotReady
	}
	current := s.current
	generation := s.generation
	s.mu.RUnlock()
	stream, err := current.Subscribe(ctx, opts)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, errors.New("cdc source returned a nil stream")
	}

	s.mu.RLock()
	stillCurrent := s.state == slotRunning && s.current == current && s.generation == generation
	s.mu.RUnlock()
	if !stillCurrent {
		stream.Close()
		return nil, api.ErrSourceNotReady
	}
	return newStampedStream(s.id, generation, opts.Buffer, stream), nil
}

// Start is idempotent while the active generation is running. This is what
// permits an update to synchronously start a candidate before the supervisor
// receives its unchanged stable slot pointer.
func (s *sourceSlot) Start(ctx context.Context) (<-chan any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	if s.disposing {
		s.mu.Unlock()
		return nil, ErrSourceBusy
	}
	if s.state == slotRunning {
		status := s.status
		s.mu.Unlock()
		return status, nil
	}
	if s.state == slotStopping {
		s.mu.Unlock()
		return nil, ErrSourceBusy
	}
	if s.current == nil {
		s.mu.Unlock()
		return nil, ErrSourceClosed
	}
	current := s.current
	status := make(chan any, 8)
	s.status = status
	s.statusDone = false
	s.state = slotStarting
	runCtx, runCancel := detachedContext(ctx)
	s.runCtx = runCtx
	s.runCancel = runCancel
	s.replacing = false
	s.mu.Unlock()

	underlying, err := startSource(ctx, runCtx, current)
	if err != nil {
		_ = stopSource(ctx, current)
		s.mu.Lock()
		s.state = slotFaulted
		s.closeStatusLocked()
		s.mu.Unlock()
		return nil, err
	}

	s.mu.Lock()
	s.state = slotRunning
	generation := s.generation
	s.mu.Unlock()
	s.watchStatus(current, generation, underlying)
	return status, nil
}

func (s *sourceSlot) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	if s.disposing {
		if s.state == slotStopped || s.state == slotFaulted {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		return ErrSourceBusy
	}
	if s.state == slotStopped {
		s.mu.Unlock()
		return nil
	}
	s.state = slotStopping
	current := s.current
	cancel := s.runCancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	err := stopSource(ctx, current)

	s.mu.Lock()
	if err != nil {
		s.state = slotFaulted
	} else {
		s.state = slotStopped
	}
	s.closeStatusLocked()
	s.runCtx = nil
	s.runCancel = nil
	s.replacing = false
	s.mu.Unlock()
	return err
}

// Dispose performs a committed delete. It mirrors Stop's stable-slot state
// transition, but delegates the destructive hook to the active driver only
// on this path. Updates and supervisor restarts always use Stop instead.
func (s *sourceSlot) Dispose(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	s.disposing = true
	s.state = slotStopping
	current := s.current
	cancel := s.runCancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var err error
	if disposable, ok := current.(Disposable); ok {
		err = disposable.Dispose(ctx)
	} else {
		err = stopSource(ctx, current)
	}

	s.mu.Lock()
	if err != nil {
		s.state = slotFaulted
	} else {
		s.state = slotStopped
	}
	s.closeStatusLocked()
	s.runCtx = nil
	s.runCancel = nil
	s.replacing = false
	s.mu.Unlock()
	return err
}

// Replace starts a candidate before changing visibility whenever the slot is
// running or the candidate is configured for auto-start. Failure leaves the
// old generation current and untouched.
func (s *sourceSlot) Replace(ctx context.Context, candidate ManagedSource) error {
	if candidate == nil {
		return ErrDriverRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.RLock()
	old := s.current
	state := s.state
	runCtx := s.runCtx
	runCancel := s.runCancel
	disposing := s.disposing
	s.mu.RUnlock()
	if disposing {
		return ErrSourceBusy
	}

	startCandidate := state == slotRunning || lifecycleAutoStart(candidate)
	sameExclusive := state == slotRunning && exclusiveResourceKey(old) != "" &&
		exclusiveResourceKey(old) == exclusiveResourceKey(candidate)
	var underlying <-chan any
	var err error
	oldStopped := false
	createdRunContext := false
	keepRunContext := false
	defer func() {
		if createdRunContext && !keepRunContext && runCancel != nil {
			runCancel()
		}
	}()
	if startCandidate && state == slotRunning {
		s.mu.Lock()
		s.replacing = true
		s.mu.Unlock()
	}
	if sameExclusive {
		// A source such as PostgreSQL may not start a second generation while
		// the old generation owns the same slot. Stop the old generation first;
		// if the candidate fails, restore the old generation before returning.
		if err := stopSource(ctx, old); err != nil {
			s.mu.Lock()
			s.replacing = false
			s.state = slotFaulted
			s.closeStatusLocked()
			s.mu.Unlock()
			return err
		}
		oldStopped = true
	}
	if startCandidate {
		if runCtx == nil {
			runCtx, runCancel = detachedContext(ctx)
			createdRunContext = true
		}
		underlying, err = startSource(ctx, runCtx, candidate)
		if err != nil {
			_ = stopSource(ctx, candidate)
			if sameExclusive {
				var restartErr error
				underlying, restartErr = startSource(ctx, runCtx, old)
				if restartErr == nil {
					s.mu.Lock()
					s.state = slotRunning
					s.replacing = false
					generation := s.generation
					s.mu.Unlock()
					s.watchStatus(old, generation, underlying)
					return err
				}
				s.mu.Lock()
				s.state = slotFaulted
				s.replacing = false
				s.closeStatusLocked()
				s.mu.Unlock()
				return errors.Join(err, restartErr)
			}
			s.mu.Lock()
			s.replacing = false
			s.mu.Unlock()
			return err
		}
	}

	s.mu.Lock()
	if s.state == slotStopping {
		s.mu.Unlock()
		_ = stopSource(ctx, candidate)
		return ErrSourceBusy
	}
	s.current = candidate
	s.generation++
	if startCandidate {
		keepRunContext = true
		if s.status == nil || s.statusDone {
			s.status = make(chan any, 8)
			s.statusDone = false
		}
		s.state = slotRunning
		s.runCtx = runCtx
		s.runCancel = runCancel
		s.replacing = false
		generation := s.generation
		s.mu.Unlock()
		s.watchStatus(candidate, generation, underlying)
	} else {
		s.mu.Unlock()
	}

	if old != nil && !oldStopped {
		if err := stopSource(ctx, old); err != nil {
			s.log.Warn("old cdc source failed to stop after replacement",
				zap.String("id", s.id.String()), zap.Error(err))
		}
	}
	return nil
}

func (s *sourceSlot) LifecycleConfig() supervisor.LifecycleConfig {
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()
	if configured, ok := current.(interface {
		LifecycleConfig() supervisor.LifecycleConfig
	}); ok {
		return configured.LifecycleConfig()
	}
	return supervisor.LifecycleConfig{}
}

func (s *sourceSlot) watchStatus(source ManagedSource, generation uint64, updates <-chan any) {
	if updates == nil {
		return
	}
	go func() {
		for detail := range updates {
			s.mu.RLock()
			current := s.current == source && s.generation == generation && s.state == slotRunning && !s.replacing
			status := s.status
			s.mu.RUnlock()
			if !current || status == nil {
				continue
			}
			select {
			case status <- detail:
			default:
			}
		}

		s.mu.Lock()
		if s.current == source && s.generation == generation && s.state == slotRunning && !s.replacing {
			s.state = slotFaulted
			s.closeStatusLocked()
		}
		s.mu.Unlock()
	}()
}

func exclusiveResourceKey(source ManagedSource) string {
	if keyed, ok := source.(ExclusiveResource); ok {
		return keyed.ExclusiveResourceKey()
	}
	return ""
}

func (s *sourceSlot) currentGeneration() uint64 {
	s.mu.RLock()
	generation := s.generation
	s.mu.RUnlock()
	return generation
}

func (s *sourceSlot) closeStatusLocked() {
	if s.status != nil && !s.statusDone {
		close(s.status)
		s.statusDone = true
	}
}

func lifecycleAutoStart(source ManagedSource) bool {
	configured, ok := source.(interface {
		LifecycleConfig() supervisor.LifecycleConfig
	})
	return ok && configured.LifecycleConfig().AutoStart
}

func sourceState(state slotState) api.SourceState {
	switch state {
	case slotStarting:
		return api.SourceStateStarting
	case slotRunning:
		return api.SourceStateRunning
	case slotFaulted:
		return api.SourceStateFaulted
	case slotStopped:
		return api.SourceStateStopped
	default:
		return api.SourceStateUnknown
	}
}

// startSource gives the startup operation its caller's cancellation while
// retaining a detached run context after successful startup. Supervisor
// start timeouts must still interrupt a blocked driver handshake, but a
// dynamic registry event must not cancel a source that it has just started.
func startSource(ctx context.Context, runCtx context.Context, source ManagedSource) (<-chan any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runCtx == nil {
		runCtx = context.WithoutCancel(ctx)
	}
	startCtx, cancelStart := context.WithCancel(runCtx)
	stopPropagation := context.AfterFunc(ctx, cancelStart)
	updates, err := source.Start(startCtx)
	stopPropagation()
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		cancelStart()
	}
	return updates, err
}

func detachedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(ctx))
}

func generationString(generation uint64) string {
	if generation == 0 {
		return ""
	}
	return strconv.FormatUint(generation, 10)
}

var _ ManagedSource = (*sourceSlot)(nil)
var _ Disposable = (*sourceSlot)(nil)
