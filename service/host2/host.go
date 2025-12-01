// Package host2 provides V2 process host using the actor scheduler.
package host2

import (
	"context"
	"errors"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pidgen"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Host implements process2.Host using the actor scheduler.
type Host struct {
	id        registry.ID
	cfg       *host.EntryConfig
	log       *zap.Logger
	scheduler *actor.Scheduler
	factory   process2.Factory
	ctx       context.Context
	statusCh  chan any

	running  atomic.Bool
	shutdown atomic.Bool
}

// NewHost creates a new V2 host with actor scheduler.
func NewHost(id registry.ID, cfg *host.EntryConfig, scheduler *actor.Scheduler, factory process2.Factory, logger *zap.Logger) *Host {
	return &Host{
		id:        id,
		cfg:       cfg,
		log:       logger,
		scheduler: scheduler,
		factory:   factory,
		statusCh:  make(chan any, 1),
	}
}

// Run implements process2.Host.
func (h *Host) Run(ctx context.Context, start *process2.Start) (relay.PID, error) {
	if h.shutdown.Load() {
		return relay.PID{}, errors.New("host is shutting down")
	}

	// Create process from factory (includes metadata with method)
	proc, meta, err := h.factory.Create(start.Source)
	if err != nil {
		return relay.PID{}, err
	}

	// Generate or use provided PID
	pid := h.preparePID(ctx, start)

	// Prepare context with hooks
	frameCtx := h.prepareContext(ctx, pid, start)

	// Get method from metadata, default to "main"
	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	// Execute OnStart hooks before scheduler submission
	if len(start.OnStart) > 0 {
		for _, hook := range start.OnStart {
			hook(frameCtx, pid, proc)
		}
	}

	// Submit to scheduler
	_, err = h.scheduler.Submit(frameCtx, pid, proc, method, start.Input)
	if err != nil {
		return relay.PID{}, err
	}

	h.log.Debug("process started",
		zap.String("pid", pid.String()),
		zap.String("source", start.Source.String()),
		zap.String("method", method))

	return pid, nil
}

// Terminate implements process2.Host.
func (h *Host) Terminate(ctx context.Context, pid relay.PID) error {
	h.log.Debug("process terminate requested", zap.String("pid", pid.String()))
	return nil
}

// Send implements relay.Receiver.
// Delegates to scheduler which routes directly to Process.Send().
func (h *Host) Send(pkg *relay.Package) error {
	if h.shutdown.Load() {
		return errors.New("host is shutting down")
	}
	return h.scheduler.Send(pkg.Target, pkg)
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

// preparePID generates a PID or uses one from options.
func (h *Host) preparePID(ctx context.Context, start *process2.Start) relay.PID {
	// Check if caller specified a PID
	if start.Options != nil {
		if pidVal, ok := start.Options.Get(process2.OptionPID); ok {
			if pid, ok := pidVal.(relay.PID); ok {
				return pid
			}
		}
	}

	// Generate new PID
	gen := pidgen.GetGenerator(ctx)
	return gen.Generate(h.id.String(), start.Source)
}

// prepareContext creates a frame context for the process.
func (h *Host) prepareContext(ctx context.Context, pid relay.PID, start *process2.Start) context.Context {
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, ctx)

	// Set standard frame keys and apply context overrides
	pairsLen := 3 + len(start.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: start.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: pid}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: h.id}
	copy(pairs[3:], start.Context)

	if err := fc.SetMultiple(pairs...); err != nil {
		h.log.Error("failed to set frame context", zap.Error(err))
	}

	// Store OnStart hooks from Start
	if len(start.OnStart) > 0 {
		if err := process2.SetOnStartHooks(pCtx, start.OnStart); err != nil {
			h.log.Error("failed to set onStart hooks", zap.Error(err))
		}
	}

	// Store OnComplete hooks from Start, adding frame cleanup
	onCompleteHooks := start.OnComplete
	onCompleteHooks = append(onCompleteHooks, func(ctx context.Context, _ relay.PID, _ *runtime.Result) {
		if fc := ctxapi.FrameFromContext(ctx); fc != nil {
			_ = fc.Close()
		}
	})
	if err := process2.SetOnCompleteHooks(pCtx, onCompleteHooks); err != nil {
		h.log.Error("failed to set onComplete hooks", zap.Error(err))
	}

	return pCtx
}
