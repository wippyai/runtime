// Package terminal2 provides terminal host using the actor scheduler.
package terminal

import (
	"context"
	"errors"
	"os"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	logsapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/system/logs"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Host implements process.Host for terminal processes using actor scheduler.
type Host struct {
	id        registry.ID
	cfg       *terminal.HostConfig
	log       *zap.Logger
	scheduler *actor.Scheduler
	factory   process.Factory
	logCtrl   *logs.ConfigSwitcher
	ctx       context.Context
	statusCh  chan any
	doneCh    chan struct{}

	running  atomic.Bool
	shutdown atomic.Bool
}

// NewHost creates a new terminal host with actor scheduler.
func NewHost(
	id registry.ID,
	cfg *terminal.HostConfig,
	scheduler *actor.Scheduler,
	factory process.Factory,
	logCtrl *logs.ConfigSwitcher,
	logger *zap.Logger,
) *Host {
	return &Host{
		id:        id,
		cfg:       cfg,
		log:       logger,
		scheduler: scheduler,
		factory:   factory,
		logCtrl:   logCtrl,
		statusCh:  make(chan any, 1),
		doneCh:    make(chan struct{}),
	}
}

// OnStart implements scheduler.Lifecycle.
func (h *Host) OnStart(ctx context.Context, pid relay.PID, proc process.Process) {}

// OnComplete implements scheduler.Lifecycle.
func (h *Host) OnComplete(ctx context.Context, pid relay.PID, result *runtime.Result) {
	h.logCtrl.RestoreBaseConfig(ctx)
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		_ = fc.Close()
	}
	close(h.doneCh)
}

// Done returns a channel that is closed when the terminal process completes.
func (h *Host) Done() <-chan struct{} {
	return h.doneCh
}

// Run implements process.Host.
func (h *Host) Run(ctx context.Context, start *process.Start) (relay.PID, error) {
	if h.shutdown.Load() {
		return relay.PID{}, errors.New("host is shutting down")
	}

	proc, meta, err := h.factory.Create(start.Source)
	if err != nil {
		return relay.PID{}, err
	}

	pid := h.preparePID(ctx, start)

	if h.cfg.HideLogs {
		if err := h.setupLogging(ctx); err != nil {
			h.log.Error("failed to setup logging", zap.Error(err))
		}
	}

	frameCtx := h.prepareContext(ctx, pid, start)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if _, err = h.scheduler.Submit(frameCtx, pid, proc, method, start.Input); err != nil {
		return relay.PID{}, err
	}

	h.log.Debug("terminal process started",
		zap.String("pid", pid.String()),
		zap.String("source", start.Source.String()),
		zap.String("method", method))

	return pid, nil
}

// Terminate implements process.Host.
func (h *Host) Terminate(ctx context.Context, pid relay.PID) error {
	h.log.Debug("process terminate requested", zap.String("pid", pid.String()))
	return nil
}

// Send implements relay.Receiver.
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

	h.log.Info("terminal host started", zap.String("id", h.id.String()))
	return h.statusCh, nil
}

// Stop implements supervisor.Service.
func (h *Host) Stop(ctx context.Context) error {
	if !h.running.Load() {
		return nil
	}

	h.shutdown.Store(true)
	h.log.Info("terminal host stopping", zap.String("id", h.id.String()))

	h.scheduler.Stop()
	h.running.Store(false)
	close(h.statusCh)

	// Restore logging on shutdown
	h.logCtrl.RestoreBaseConfig(ctx)

	h.log.Info("terminal host stopped", zap.String("id", h.id.String()))
	return nil
}

// preparePID generates a PID or uses one from options.
func (h *Host) preparePID(ctx context.Context, start *process.Start) relay.PID {
	if start.Options != nil {
		if pidVal, ok := start.Options.Get(process.OptionPID); ok {
			if pid, ok := pidVal.(relay.PID); ok {
				return pid
			}
		}
	}

	gen := process.GetPIDGenerator(ctx)
	return gen.Generate(h.id.String())
}

// prepareContext creates a frame context for the terminal process.
func (h *Host) prepareContext(ctx context.Context, pid relay.PID, start *process.Start) context.Context {
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, ctx)

	pairsLen := 4 + len(start.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: start.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: pid}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: h.id}
	pairs[3] = ctxapi.Pair{Key: terminal.TerminalCtxKey, Value: terminal.NewTerminalContext(os.Stdin, os.Stdout, os.Stderr)}
	copy(pairs[4:], start.Context)

	if err := fc.SetMultiple(pairs...); err != nil {
		h.log.Error("failed to set frame context", zap.Error(err))
	}

	return pCtx
}

// setupLogging redirects logs to event bus for terminal output.
func (h *Host) setupLogging(ctx context.Context) error {
	return h.logCtrl.EnableTemporaryConfig(ctx, logsapi.Config{
		MinLevel:            zap.DebugLevel,
		StreamToEvents:      true,
		PropagateDownstream: false,
	})
}

var _ process.Host = (*Host)(nil)
