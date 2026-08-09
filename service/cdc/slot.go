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
	runCtx      context.Context
	current     ManagedSource
	runCancel   context.CancelFunc
	log         *zap.Logger
	status      chan any
	retiredHook func(string, uint64)
	id          registry.ID
	kind        registry.Kind
	retired     []retiredSource
	generation  uint64
	mu          sync.RWMutex
	opMu        sync.Mutex
	state       slotState
	disposing   bool
	replacing   bool
	statusDone  bool
}

type retiredSource struct {
	source ManagedSource
	key    string
	token  uint64
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
	if isNilSource(current) {
		return api.SourceInfo{
			ID:         s.id,
			Kind:       s.kind,
			Name:       s.id.String(),
			Generation: generationString(generation),
			State:      sourceState(state),
			Streaming:  state == slotRunning,
			Faulted:    state == slotFaulted,
			Epoch:      generationString(generation),
		}
	}
	info := current.Info()
	info.ID = s.id
	info.Kind = s.kind
	info.Name = s.id.String()
	info.Generation = generationString(generation)
	info.State = sourceState(state)
	info.Streaming = state == slotRunning
	info.Faulted = state == slotFaulted
	if info.Generation != "" {
		info.Epoch = info.Generation
	}
	return info
}

func (s *sourceSlot) Subscribe(ctx context.Context, opts api.StreamOptions) (api.Stream, error) {
	s.mu.RLock()
	if s.state != slotRunning || isNilSource(s.current) || s.disposing || s.replacing {
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
	stillCurrent := s.state == slotRunning && s.current == current && s.generation == generation && !s.replacing
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
	if len(s.retired) > 0 {
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
	if isNilSource(s.current) {
		s.mu.Unlock()
		return nil, ErrSourceClosed
	}
	current := s.current
	restart := s.state == slotStopped || s.state == slotFaulted
	status := make(chan any, 8)
	s.status = status
	s.statusDone = false
	s.state = slotStarting
	runCtx, runCancel := detachedContext(ctx)
	s.runCtx = runCtx
	s.runCancel = runCancel
	s.replacing = false
	s.mu.Unlock()

	if err := s.retryRetired(ctx); err != nil {
		_ = stopSource(ctx, current)
		s.mu.Lock()
		s.state = slotFaulted
		s.closeStatusLocked()
		s.runCtx = nil
		s.runCancel = nil
		s.mu.Unlock()
		return nil, err
	}

	underlying, err := startSource(ctx, runCtx, current)
	if err != nil {
		_ = stopSource(ctx, current)
		s.mu.Lock()
		s.state = slotFaulted
		s.closeStatusLocked()
		s.runCtx = nil
		s.runCancel = nil
		s.mu.Unlock()
		return nil, err
	}

	s.mu.Lock()
	s.state = slotRunning
	if restart {
		s.generation++
	}
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
	if !s.disposing && s.state == slotStopped && len(s.retired) == 0 {
		s.mu.Unlock()
		return nil
	}
	s.state = slotStopping
	current := s.current
	cancel := s.runCancel
	pendingCurrent := s.isRetiredLocked(current)
	disposing := s.disposing
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var err error
	if disposing && !pendingCurrent {
		if disposable, ok := current.(Disposable); ok {
			err = disposable.Dispose(ctx)
		} else {
			err = stopSource(ctx, current)
		}
	} else {
		err = stopSource(ctx, current)
	}
	if err == nil {
		err = s.retryRetired(ctx)
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
	pendingCurrent := s.isRetiredLocked(current)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	var err error
	if isNilSource(current) {
		err = ErrSourceClosed
	} else if !pendingCurrent {
		if disposable, ok := current.(Disposable); ok {
			err = disposable.Dispose(ctx)
		} else {
			err = stopSource(ctx, current)
		}
	} else {
		err = nil
	}
	if err == nil {
		err = s.retryRetired(ctx)
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
func (s *sourceSlot) Replace(ctx context.Context, candidate ManagedSource, retiredTokens ...uint64) error {
	if isNilSource(candidate) {
		return ErrDriverRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.Lock()
	if s.disposing || len(s.retired) > 0 {
		s.mu.Unlock()
		_ = stopUnstartedSource(ctx, candidate)
		return ErrSourceBusy
	}
	old := s.current
	oldState := s.state
	oldRunCancel := s.runCancel
	if oldState == slotStopping {
		s.mu.Unlock()
		_ = stopUnstartedSource(ctx, candidate)
		return ErrSourceBusy
	}
	s.replacing = true
	s.mu.Unlock()

	retiredToken := uint64(0)
	if len(retiredTokens) > 0 {
		retiredToken = retiredTokens[0]
	}
	oldKey := exclusiveResourceKey(old)
	candidateKey := exclusiveResourceKey(candidate)
	differentResource := oldKey != candidateKey
	startCandidate := oldState == slotRunning || lifecycleAutoStart(candidate)
	shouldStopOld := !isNilSource(old) && (oldState != slotStopped && oldState != slotIdle || differentResource || oldKey == "")

	var (
		underlying <-chan any
		runCtx     context.Context
		runCancel  context.CancelFunc
	)
	speculative := differentResource && startCandidate
	startCandidateGeneration := func() error {
		runCtx, runCancel = detachedContext(ctx)
		var err error
		underlying, err = startSource(ctx, runCtx, candidate)
		if err != nil {
			_ = cleanupStartedSource(ctx, candidate, differentResource)
			runCancel()
		}
		return err
	}
	if speculative {
		// Different resource keys may be prepared in parallel. The candidate
		// remains private until old Stop and Dispose both commit below.
		if err := startCandidateGeneration(); err != nil {
			return s.resetReplaceFailure(oldState, err)
		}
	}

	// The old source is stopped before destructive cleanup. A speculative
	// candidate may already be running, but it is not visible through the slot
	// until this handoff has completed.
	oldStopped := !shouldStopOld
	if shouldStopOld {
		if err := stopGeneration(ctx, old, oldRunCancel); err != nil {
			if speculative {
				runCancel()
				_ = cleanupStartedSource(ctx, candidate, differentResource)
			}
			s.mu.Lock()
			s.state = slotFaulted
			s.replacing = false
			s.closeStatusLocked()
			s.runCtx = nil
			s.runCancel = nil
			s.mu.Unlock()
			if !speculative {
				_ = stopUnstartedSource(ctx, candidate)
			}
			return err
		}
		oldStopped = true
		s.mu.Lock()
		s.state = slotStopped
		s.runCtx = nil
		s.runCancel = nil
		s.closeStatusLocked()
		s.mu.Unlock()
	}

	if differentResource && !isNilSource(old) {
		if disposable, ok := old.(Disposable); ok {
			if err := disposable.Dispose(ctx); err != nil {
				// Keep the old source current and retain its lease. Stop retries
				// this pending destructive cleanup during shutdown or delete.
				s.recordRetired(old, oldKey, retiredToken)
				s.mu.Lock()
				s.state = slotFaulted
				s.replacing = false
				s.mu.Unlock()
				if speculative {
					runCancel()
				}
				_ = cleanupStartedSource(ctx, candidate, speculative && differentResource)
				return err
			}
			if oldKey != "" {
				s.mu.RLock()
				hook := s.retiredHook
				s.mu.RUnlock()
				if hook != nil {
					hook(oldKey, retiredToken)
				}
			}
		}
	}

	if startCandidate && !speculative {
		if err := startCandidateGeneration(); err != nil {
			_, oldDisposable := old.(Disposable)
			restoreOld := oldState == slotRunning && (!differentResource || !oldDisposable)
			if restoreOld && oldStopped {
				// The old generation still owns the shared resource. Restore it
				// before making the failed update visible to the caller.
				restoreCtx, restoreCancel := detachedContext(ctx)
				oldUpdates, restoreErr := startSource(ctx, restoreCtx, old)
				if restoreErr == nil {
					s.mu.Lock()
					s.state = slotRunning
					s.generation++
					s.runCtx = restoreCtx
					s.runCancel = restoreCancel
					s.replacing = false
					generation := s.generation
					if s.status == nil || s.statusDone {
						s.status = make(chan any, 8)
						s.statusDone = false
					}
					s.mu.Unlock()
					s.watchStatus(old, generation, oldUpdates)
					return err
				}
				restoreCancel()
				return s.finishReplaceFailure(errors.Join(err, restoreErr))
			}
			return s.finishReplaceFailure(err)
		}
	}

	s.mu.Lock()
	if s.disposing || s.state == slotStopping {
		s.mu.Unlock()
		if runCancel != nil {
			runCancel()
		}
		_ = cleanupStartedSource(ctx, candidate, startCandidate && differentResource)
		return ErrSourceBusy
	}
	s.current = candidate
	s.generation++
	if startCandidate {
		if s.status == nil || s.statusDone {
			s.status = make(chan any, 8)
			s.statusDone = false
		}
		s.state = slotRunning
		s.runCtx = runCtx
		s.runCancel = runCancel
	} else if oldState == slotIdle {
		s.state = slotIdle
	} else {
		s.state = slotStopped
	}
	s.replacing = false
	generation := s.generation
	s.mu.Unlock()

	if startCandidate {
		s.watchStatus(candidate, generation, underlying)
	}
	return nil
}

func (s *sourceSlot) finishReplaceFailure(err error) error {
	s.mu.Lock()
	s.state = slotFaulted
	s.closeStatusLocked()
	s.runCtx = nil
	s.runCancel = nil
	s.replacing = false
	s.mu.Unlock()
	return err
}

func (s *sourceSlot) resetReplaceFailure(state slotState, err error) error {
	s.mu.Lock()
	s.state = state
	s.replacing = false
	s.mu.Unlock()
	return err
}

// stopUnstartedSource abandons a source returned by Driver.Create before its
// Start method has successfully handed ownership of a durable resource to the
// manager. Drivers must make Create side-effect-free; Stop is intentionally
// the only cleanup allowed on this path so an unstarted candidate cannot drop
// a shared replication slot/checkpoint.
func stopUnstartedSource(ctx context.Context, source api.Source) error {
	return stopSource(ctx, source)
}

// cleanupStartedSource cleans a candidate after Start was attempted. A
// different exclusive resource is owned solely by that candidate and may be
// destructively disposed. Same-key candidates share the old resource contract
// and must only be stopped; disposing them could drop the old generation's
// resource.
func cleanupStartedSource(ctx context.Context, source api.Source, destructive bool) error {
	if destructive {
		return disposeSource(ctx, source)
	}
	return stopSource(ctx, source)
}

func disposeSource(ctx context.Context, source api.Source) error {
	if isNilSource(source) {
		return nil
	}
	if disposable, ok := source.(Disposable); ok {
		return disposable.Dispose(ctx)
	}
	return stopSource(ctx, source)
}

func stopGeneration(ctx context.Context, source api.Source, cancel context.CancelFunc) error {
	if cancel != nil {
		cancel()
	}
	return stopSource(ctx, source)
}

func (s *sourceSlot) recordRetired(source ManagedSource, key string, token uint64) {
	if isNilSource(source) {
		return
	}
	s.mu.Lock()
	s.retired = append(s.retired, retiredSource{source: source, key: key, token: token})
	s.mu.Unlock()
}

func (s *sourceSlot) isRetiredLocked(source ManagedSource) bool {
	if isNilSource(source) {
		return false
	}
	for _, retired := range s.retired {
		if retired.source == source {
			return true
		}
	}
	return false
}

func (s *sourceSlot) retryRetired(ctx context.Context) error {
	for {
		s.mu.RLock()
		if len(s.retired) == 0 {
			s.mu.RUnlock()
			return nil
		}
		retired := s.retired[0]
		s.mu.RUnlock()

		if err := disposeSource(ctx, retired.source); err != nil {
			return err
		}

		s.mu.Lock()
		if len(s.retired) > 0 && s.retired[0].source == retired.source {
			s.retired = s.retired[1:]
		}
		hook := s.retiredHook
		s.mu.Unlock()
		if hook != nil && retired.key != "" {
			hook(retired.key, retired.token)
		}
	}
}

func (s *sourceSlot) setRetiredCleanupHook(hook func(string, uint64)) {
	s.mu.Lock()
	s.retiredHook = hook
	s.mu.Unlock()
}

func (s *sourceSlot) hasRetiredKey(key string) bool {
	if key == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, retired := range s.retired {
		if retired.key == key {
			return true
		}
	}
	return false
}

func (s *sourceSlot) resourceKeys() []string {
	s.mu.RLock()
	current := s.current
	retired := append([]retiredSource(nil), s.retired...)
	s.mu.RUnlock()
	keys := make([]string, 0, len(retired)+1)
	if key := exclusiveResourceKey(current); key != "" {
		keys = append(keys, key)
	}
	for _, item := range retired {
		if item.key == "" {
			continue
		}
		seen := false
		for _, key := range keys {
			if key == item.key {
				seen = true
				break
			}
		}
		if !seen {
			keys = append(keys, item.key)
		}
	}
	return keys
}

func (s *sourceSlot) LifecycleConfig() supervisor.LifecycleConfig {
	s.mu.RLock()
	current := s.current
	s.mu.RUnlock()
	if isNilSource(current) {
		return supervisor.LifecycleConfig{}
	}
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
	if isNilSource(source) {
		return ""
	}
	if keyed, ok := source.(ExclusiveResource); ok {
		return keyed.ExclusiveResourceKey()
	}
	return ""
}

func (s *sourceSlot) currentSource() ManagedSource {
	s.mu.RLock()
	source := s.current
	s.mu.RUnlock()
	return source
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
