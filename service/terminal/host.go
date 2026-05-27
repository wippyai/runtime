// SPDX-License-Identifier: MPL-2.0

// Package terminal provides terminal host using the actor scheduler.
package terminal

import (
	"context"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	ctxapi "github.com/wippyai/runtime/api/context"
	logsapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	terminalapi "github.com/wippyai/runtime/api/service/terminal"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/logs"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// Host implements process.Host for terminal processes using actor scheduler.
type Host struct {
	factory      process.Factory
	ctx          context.Context
	cfg          *terminalapi.HostConfig
	log          *zap.Logger
	scheduler    *actor.Scheduler
	logCtrl      *logs.Configurator
	raw          *RawManager
	statusCh     chan any
	doneCh       chan struct{}
	id           registry.ID
	running      atomic.Bool
	shutdown     atomic.Bool
	stopCalls    atomic.Uint64
	lifecycleMu  sync.RWMutex
	statusClosed bool
	doneClosed   bool
}

// NewHost creates a new terminal host with actor scheduler.
func NewHost(
	id registry.ID,
	cfg *terminalapi.HostConfig,
	scheduler *actor.Scheduler,
	factory process.Factory,
	logCtrl *logs.Configurator,
	logger *zap.Logger,
) *Host {
	if logger == nil {
		logger = zap.NewNop()
	}
	if logCtrl == nil {
		logCtrl = logs.NewConfigurator(nil, logger)
	}
	return &Host{
		id:        id,
		cfg:       cfg,
		log:       logger,
		scheduler: scheduler,
		factory:   factory,
		logCtrl:   logCtrl,
		statusCh:  make(chan any, 1),
		doneCh:    make(chan struct{}),
		raw:       NewRawManager(os.Stdin),
	}
}

// OnStart implements scheduler.Lifecycle.
func (h *Host) OnStart(context.Context, pid.PID, process.Process) error { return nil }

// OnComplete implements scheduler.Lifecycle.
func (h *Host) OnComplete(ctx context.Context, _ pid.PID, result *runtime.Result) {
	h.logCtrl.RestoreBaseConfig(ctx)
	if tc := terminalapi.GetTerminalContext(ctx); tc != nil && tc.Input != nil {
		_ = tc.Input.Stop()
	}
	if h.raw != nil {
		_ = h.raw.Reset()
	}
	if fc := ctxapi.FrameFromContext(ctx); fc != nil {
		_ = fc.Close()
	}
	h.closeDone()

	exitCode, output := completionExitCode(result)
	if result != nil && result.Error != nil {
		// Print error to stderr, deduplicate if message is repeated
		errStr := result.Error.Error()
		if idx := strings.Index(errStr, ": "); idx > 0 {
			prefix := errStr[:idx+2]
			rest := errStr[idx+2:]
			if strings.HasPrefix(rest, prefix) {
				errStr = rest
			}
		}
		_, _ = os.Stderr.WriteString(errStr + "\n")
	} else if output != "" {
		_, _ = os.Stdout.WriteString(output + "\n")
	}
	supervisorapi.TriggerShutdown(ctx, exitCode)
}

func completionExitCode(result *runtime.Result) (int, string) {
	if result == nil {
		return 0, ""
	}
	if result.Error != nil {
		return 1, ""
	}
	if result.Value == nil {
		return 0, ""
	}

	data := result.Value.Data()
	if s, ok := data.(string); ok {
		return 0, s
	}
	if code, ok := numericExitCode(data); ok {
		return code, ""
	}
	if code, ok := boolExitCode(data); ok {
		return code, ""
	}
	return 0, ""
}

func numericExitCode(data any) (int, bool) {
	if data == nil {
		return 0, false
	}

	v := reflect.ValueOf(data)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := v.Uint()
		maxInt := uint64(^uint(0) >> 1)
		if u > maxInt {
			return 1, true
		}
		return int(u), true
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 1, true
		}
		return int(f), true
	default:
		return 0, false
	}
}

func boolExitCode(data any) (int, bool) {
	if data == nil {
		return 0, false
	}

	v := reflect.ValueOf(data)
	if v.Kind() != reflect.Bool {
		return 0, false
	}
	if v.Bool() {
		return 0, true
	}
	return 1, true
}

// Done returns a channel that is closed when the terminal process completes.
func (h *Host) Done() <-chan struct{} {
	h.lifecycleMu.RLock()
	defer h.lifecycleMu.RUnlock()
	return h.doneCh
}

// Run implements process.Host.
func (h *Host) Run(ctx context.Context, start *process.Start) (pid.PID, error) {
	if !h.running.Load() {
		return pid.PID{}, ErrHostNotRunning
	}
	if h.shutdown.Load() {
		return pid.PID{}, ErrHostShuttingDown
	}

	proc, meta, err := h.factory.Create(start.Source)
	if err != nil {
		return pid.PID{}, err
	}

	processID := h.preparePID(ctx, start)

	if h.cfg.HideLogs {
		if err := h.setupLogging(ctx); err != nil {
			h.log.Error("failed to setup logging", zap.Error(err))
		}
	}

	frameCtx := h.prepareContext(ctx, processID, start)

	method := "main"
	if meta != nil && meta.Method != "" {
		method = meta.Method
	}

	if _, err = h.scheduler.Submit(frameCtx, processID, proc, method, start.Input); err != nil {
		proc.Close()
		if fc := ctxapi.FrameFromContext(frameCtx); fc != nil {
			ctxapi.ReleaseFrameContext(fc)
		}
		return pid.PID{}, err
	}

	h.log.Debug("terminal process started",
		zap.String("pid", processID.String()),
		zap.String("source", start.Source.String()),
		zap.String("method", method))

	return processID, nil
}

// Terminate implements process.Host.
func (h *Host) Terminate(_ context.Context, processID pid.PID) error {
	h.log.Debug("process terminate requested", zap.String("pid", processID.String()))
	return nil
}

// Send implements relay.Receiver.
func (h *Host) Send(pkg *relay.Package) error {
	if h.shutdown.Load() {
		return ErrHostShuttingDown
	}
	return h.scheduler.Send(pkg)
}

// Start implements supervisor.Service.
func (h *Host) Start(ctx context.Context) (<-chan any, error) {
	if h.running.Swap(true) {
		return nil, ErrHostAlreadyRunning
	}

	h.lifecycleMu.Lock()
	h.ctx = ctx
	h.shutdown.Store(false)
	// Recreate lifecycle channels on each start so stop/restart cycles
	// don't reuse closed channels from a previous run.
	h.statusCh = make(chan any, 1)
	h.doneCh = make(chan struct{})
	h.statusClosed = false
	h.doneClosed = false
	statusCh := h.statusCh
	h.lifecycleMu.Unlock()
	h.scheduler.Start()

	h.log.Info("terminal host started", zap.String("id", h.id.String()))
	return statusCh, nil
}

// Stop implements supervisor.Service.
func (h *Host) Stop(ctx context.Context) error {
	stopAttempt := h.stopCalls.Add(1)
	if !h.running.Swap(false) {
		h.log.Warn("terminal host stop requested while already stopped",
			zap.String("id", h.id.String()),
			zap.Uint64("attempt", stopAttempt),
			zap.Bool("shutdown", h.shutdown.Load()))
		return nil
	}

	h.shutdown.Store(true)
	h.log.Info("terminal host stopping",
		zap.String("id", h.id.String()),
		zap.Uint64("attempt", stopAttempt))

	h.scheduler.Stop(ctx)
	h.closeStatus()

	if h.raw != nil {
		_ = h.raw.Reset()
	}
	// Restore logging on shutdown
	h.logCtrl.RestoreBaseConfig(ctx)

	h.log.Info("terminal host stopped", zap.String("id", h.id.String()))
	return nil
}

// preparePID generates a PID for the process.
func (h *Host) preparePID(ctx context.Context, start *process.Start) pid.PID {
	_ = start
	gen := process.GetPIDGenerator(ctx)
	return gen.Generate(h.id.String())
}

// prepareContext creates a frame context for the terminal process.
func (h *Host) prepareContext(ctx context.Context, processID pid.PID, start *process.Start) context.Context {
	pCtx, fc := ctxapi.OpenFrameContextOn(h.ctx, ctx)

	// Extract args from Input payloads
	var args []string
	for _, p := range start.Input {
		if s, ok := p.Data().(string); ok {
			args = append(args, s)
		}
	}

	pairsLen := 3 + len(start.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: start.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: processID}
	tc := terminalapi.NewTerminalContextWithArgs(os.Stdin, os.Stdout, os.Stderr, args)
	tc.Raw = h.raw
	tc.Input = NewInputReader(os.Stdin, h.raw, h.scheduler, processID)
	pairs[2] = ctxapi.Pair{Key: terminalapi.Key(), Value: tc}
	copy(pairs[3:], start.Context)

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

func (h *Host) closeStatus() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.statusClosed {
		return
	}
	close(h.statusCh)
	h.statusClosed = true
}

func (h *Host) closeDone() {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.doneClosed {
		return
	}
	close(h.doneCh)
	h.doneClosed = true
}

var _ process.Host = (*Host)(nil)
