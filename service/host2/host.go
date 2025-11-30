// Package host2 provides V2 process host using the actor scheduler.
package host2

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Host implements process.Managed using the actor scheduler.
type Host struct {
	id        registry.ID
	cfg       *host.EntryConfig
	log       *zap.Logger
	scheduler *actor.Scheduler
	ctx       context.Context
	statusCh  chan any

	running  atomic.Bool
	shutdown atomic.Bool

	// Process tracking for relay
	mu       sync.RWMutex
	channels map[relay.PID]chan *relay.Package
}

// NewHost creates a new V2 host with actor scheduler.
func NewHost(id registry.ID, cfg *host.EntryConfig, scheduler *actor.Scheduler, logger *zap.Logger) *Host {
	return &Host{
		id:        id,
		cfg:       cfg,
		log:       logger,
		scheduler: scheduler,
		statusCh:  make(chan any, 1),
		channels:  make(map[relay.PID]chan *relay.Package),
	}
}

// Launch implements process.Managed.
func (h *Host) Launch(ctx context.Context, launch *process.Launch) (relay.PID, error) {
	if h.shutdown.Load() {
		return relay.PID{}, errors.New("host is shutting down")
	}

	// Only V2 processes supported
	v2proc, ok := launch.Process.(process2.Process)
	if !ok {
		return relay.PID{}, errors.New("process must implement process2.Process")
	}

	// Prepare context with hooks
	frameCtx := h.prepareContext(ctx, launch)

	// Get method from options
	method := "main"
	if launch.Options != nil {
		if m := launch.Options.GetString("method", ""); m != "" {
			method = m
		}
	}

	// Execute OnStart hooks before scheduler submission
	if len(launch.OnStart) > 0 {
		for _, hook := range launch.OnStart {
			hook(frameCtx, launch.PID, launch.Process)
		}
	}

	// Submit to scheduler
	_, err := h.scheduler.Submit(frameCtx, launch.PID, v2proc, method, launch.Input)
	if err != nil {
		return relay.PID{}, err
	}

	h.log.Debug("process launched",
		zap.String("pid", launch.PID.String()),
		zap.String("source", launch.Source.String()))

	return launch.PID, nil
}

// Terminate implements process.Host.
func (h *Host) Terminate(ctx context.Context, pid relay.PID) error {
	// Scheduler will call OnComplete hook when process finishes
	// Just log termination request
	h.log.Debug("process terminate requested", zap.String("pid", pid.String()))
	return nil
}

// Send implements relay.Receiver.
func (h *Host) Send(pkg *relay.Package) error {
	if h.shutdown.Load() {
		return errors.New("host is shutting down")
	}

	h.mu.RLock()
	ch, ok := h.channels[pkg.Target]
	h.mu.RUnlock()

	if !ok {
		return errors.New("process not found")
	}

	select {
	case ch <- pkg:
		return nil
	default:
		return errors.New("process channel full")
	}
}

// Attach implements relay.Host.
func (h *Host) Attach(pid relay.PID, ch chan *relay.Package) (context.CancelFunc, error) {
	if h.shutdown.Load() {
		return nil, errors.New("host is shutting down")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.channels[pid]; exists {
		return nil, relay.ErrAlreadyAttached
	}

	h.channels[pid] = ch

	return func() {
		h.Detach(pid)
	}, nil
}

// Detach implements relay.Host.
func (h *Host) Detach(pid relay.PID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.channels, pid)
}

// Start implements supervisor.Service.
func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	if h.running.Swap(true) {
		return nil, errors.New("host already running")
	}

	h.ctx = ctx
	h.scheduler.Start()

	h.log.Info("host started", zap.String("id", h.id.String()))
	return h.statusCh, nil
}

// Stop implements supervisor.Service.
func (h *Host) Stop(ctx context.Context) error {
	if !h.running.Load() {
		return nil
	}

	h.shutdown.Store(true)
	h.log.Info("host stopping", zap.String("id", h.id.String()))

	h.scheduler.Stop()
	h.running.Store(false)
	close(h.statusCh)

	h.log.Info("host stopped", zap.String("id", h.id.String()))
	return nil
}

// prepareContext creates a frame context for the process.
func (h *Host) prepareContext(ctx context.Context, launch *process.Launch) context.Context {
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, ctx)

	// Set standard frame keys and apply context overrides
	pairsLen := 3 + len(launch.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: launch.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: launch.PID}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: h.id}
	copy(pairs[3:], launch.Context)

	if err := fc.SetMultiple(pairs...); err != nil {
		h.log.Error("failed to set frame context", zap.Error(err))
	}

	// Store OnStart hooks from Launch
	if len(launch.OnStart) > 0 {
		if err := process.SetOnStartHooks(pCtx, launch.OnStart); err != nil {
			h.log.Error("failed to set onStart hooks", zap.Error(err))
		}
	}

	// Store OnComplete hooks from Launch, adding frame cleanup
	onCompleteHooks := launch.OnComplete
	onCompleteHooks = append(onCompleteHooks, func(ctx context.Context, _ relay.PID, _ *runtime.Result) {
		if fc := ctxapi.FrameFromContext(ctx); fc != nil {
			_ = fc.Close()
		}
	})
	if err := process.SetOnCompleteHooks(pCtx, onCompleteHooks); err != nil {
		h.log.Error("failed to set onComplete hooks", zap.Error(err))
	}

	return pCtx
}
