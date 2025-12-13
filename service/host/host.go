// Package host provides process host implementation using the actor scheduler.
package host

import (
	"context"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Host implements process.Host using the actor scheduler.
type Host struct {
	id        registry.ID
	cfg       *host.EntryConfig
	log       *zap.Logger
	scheduler *actor.Scheduler
	factory   process.Factory
	pidGen    *uniqid.PIDGenerator
	ctx       context.Context

	running  atomic.Bool
	shutdown atomic.Bool
}

// NewHost creates a new host with actor scheduler.
func NewHost(id registry.ID, cfg *host.EntryConfig, scheduler *actor.Scheduler, factory process.Factory, pidGen *uniqid.PIDGenerator, logger *zap.Logger) *Host {
	return &Host{
		id:        id,
		cfg:       cfg,
		log:       logger,
		scheduler: scheduler,
		factory:   factory,
		pidGen:    pidGen,
	}
}

// Run implements process.Host.
func (h *Host) Run(ctx context.Context, start *process.Start) (pid.PID, error) {
	if !h.running.Load() {
		return pid.PID{}, host.ErrHostNotRunning
	}
	if h.shutdown.Load() {
		return pid.PID{}, host.ErrHostShuttingDown
	}

	proc, meta, err := h.factory.Create(start.Source)
	if err != nil {
		return pid.PID{}, err
	}

	processID := h.preparePID(ctx, start)
	frameCtx := h.prepareContext(ctx, processID, start)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if _, err = h.scheduler.Submit(frameCtx, processID, proc, method, start.Input); err != nil {
		proc.Close()
		return pid.PID{}, err
	}

	h.log.Debug("process started",
		zap.String("pid", processID.String()),
		zap.String("source", start.Source.String()),
		zap.String("method", method))

	return processID, nil
}

// Terminate implements process.Host.
func (h *Host) Terminate(_ context.Context, processID pid.PID) error {
	h.log.Debug("process terminate requested", zap.String("pid", processID.String()))
	return h.scheduler.Terminate(processID)
}

// Send implements relay.Receiver.
func (h *Host) Send(pkg *relay.Package) error {
	if h.shutdown.Load() {
		return host.ErrHostShuttingDown
	}
	return h.scheduler.Send(pkg)
}

// Start implements supervisor.Service.
func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	if h.running.Swap(true) {
		return nil, host.ErrHostAlreadyRunning
	}

	h.ctx = ctx
	h.scheduler.Start()

	h.log.Info("host started", zap.String("id", h.id.String()))
	return nil, nil
}

// Stop implements supervisor.Service.
func (h *Host) Stop(ctx context.Context) error {
	if !h.running.Swap(false) {
		return nil
	}

	h.shutdown.Store(true)
	h.log.Info("host stopping", zap.String("id", h.id.String()))

	h.scheduler.Stop(ctx)

	h.log.Info("host stopped", zap.String("id", h.id.String()))
	return nil
}

// preparePID generates a PID or uses one from options.
func (h *Host) preparePID(_ context.Context, start *process.Start) pid.PID {
	if start.Options != nil {
		if pidVal, ok := start.Options.Get(process.OptionPID); ok {
			if processID, ok := pidVal.(pid.PID); ok {
				return processID
			}
		}
	}

	return h.pidGen.Generate(h.id.String())
}

// prepareContext creates a frame context for the process.
func (h *Host) prepareContext(ctx context.Context, processID pid.PID, start *process.Start) context.Context {
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, ctx)

	pairsLen := 3 + len(start.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: start.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: processID}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameLifecycleOptionsKey, Value: start.Options}
	copy(pairs[3:], start.Context)

	if err := fc.SetMultiple(pairs...); err != nil {
		h.log.Error("failed to set frame context", zap.Error(err))
	}

	return pCtx
}

// OnStart implements scheduler.Lifecycle.
// Host-specific lifecycle is empty; global lifecycle handles process registration.
func (h *Host) OnStart(_ context.Context, _ pid.PID, _ process.Process) {}

// OnComplete implements scheduler.Lifecycle.
func (h *Host) OnComplete(ctx context.Context, _ pid.PID, _ *runtime.Result) {
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		ctxapi.ReleaseFrameContext(fc)
	}
}

var _ process.Host = (*Host)(nil)
