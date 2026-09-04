// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"context"
	"errors"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
	"strconv"
	"sync"
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
	source      ManagedSource
	key         string
	token       uint64
	destructive bool
}

// leaseRef is passed through a replacement handoff so a failed private
// candidate can retain exactly the lease it reserved. The manager owns the
// lease map; the slot only reports successful retired cleanup through its
// hook.
type leaseRef struct {
	key   string
	token uint64
	owned bool
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

// Subscribe delegates pre-start subscriptions to drivers that can retain the
// registration until Start establishes the generation. This is required for
// source-owned startup snapshots; drivers without that handoff return
// ErrSourceNotReady. The stable slot still rejects stopped, replacing, and
// disposing generations.
func (s *sourceSlot) Subscribe(ctx context.Context, opts api.StreamOptions) (api.Stream, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	preStart := s.state == slotIdle || s.state == slotStarting
	if (!preStart && s.state != slotRunning) || isNilSource(s.current) || s.disposing || s.replacing {
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
	stillCurrent := (s.state == slotIdle || s.state == slotStarting || s.state == slotRunning) &&
		s.current == current && s.generation == generation && !s.replacing
	s.mu.RUnlock()
	if !stillCurrent {
		stream.Close()
		return nil, api.ErrSourceNotReady
	}
	return newStampedStream(s.id, generation, stream), nil
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
	err = errors.Join(err, s.retryRetired(ctx))

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
	err = errors.Join(err, s.retryRetired(ctx))

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
