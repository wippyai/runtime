package terminal

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	logsapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/logs"
	"go.uber.org/zap"
)

type opType int

const (
	opLaunch opType = iota
	opTerminate
	opSend
	opUpdateConfig
)

type op struct {
	typ      opType
	ctx      context.Context
	launch   *process.Launch
	msg      *relay.Package
	cfg      *terminal.HostConfig
	result   chan error
	response chan relay.PID
	// For attach operation
}

// Terminal manages a terminal session and hosts terminal processes.
// It implements the process.Host interface to allow running terminal processes,
// and the supervisor.Service interface to manage its lifecycle.
type Terminal struct {
	id            registry.ID
	ctx           context.Context
	config        *terminal.HostConfig
	opCh          chan op
	done          chan struct{}
	logCtrl       *logs.ConfigSwitcher
	log           *zap.Logger
	runner        atomic.Pointer[Runner]
	runnerFactory RunnerFactory
}

// NewTerminalHost creates a new Terminal with custom runner factory.
func NewTerminalHost(
	id registry.ID,
	cfg *terminal.HostConfig,
	logCtrl *logs.ConfigSwitcher,
	log *zap.Logger,
	runnerFactory RunnerFactory,
) *Terminal {
	return &Terminal{
		id:            id,
		config:        cfg,
		opCh:          make(chan op, 10),
		done:          make(chan struct{}),
		logCtrl:       logCtrl,
		log:           log,
		runnerFactory: runnerFactory,
	}
}

// Start begins the terminal service and returns a status channel.
// It implements the supervisor.Service interface.
func (t *Terminal) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)
	go t.run(ctx, status)
	status <- "started"
	return status, nil
}

func (t *Terminal) run(ctx context.Context, status chan<- any) {
	defer close(t.done)
	defer close(status)
	defer t.cleanup(nil)

	t.ctx = ctx

	for {
		select {
		case <-ctx.Done():
			return
		case op := <-t.opCh:
			var err error

			switch op.typ {
			case opLaunch:
				err = t.handleLaunch(op.ctx, op.launch, op.response)
			case opTerminate:
				err = t.handleTerminate()
			case opSend:
				err = t.handleSend(op.msg)
			case opUpdateConfig:
				t.config = op.cfg
				t.log.Info("config updated")
			}

			if op.result != nil {
				select {
				case op.result <- err:
				default:
					t.log.Warn("failed to send operation result")
				}
			}
		}
	}
}

func (t *Terminal) handleLaunch(ctx context.Context, pl *process.Launch, response chan relay.PID) error {
	if t.runner.Load() != nil {
		close(response)
		return process.ErrHostBusy
	}

	if t.config.HideLogs {
		if err := t.setupLogging(); err != nil {
			close(response)
			return err
		}
	}

	// Use Terminal's context, not the calling context - processes live with the Terminal, not the caller
	rCtx := t.prepareContext(ctx, pl)

	cfg := &RunnerConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	// Use the runner factory to create a runner
	runner, err := t.runnerFactory.CreateRunner(rCtx, cfg, pl)
	if err != nil {
		t.log.Error("failed to create terminal runner",
			zap.Error(err))

		t.cleanup(nil)
		close(response)
		return err
	}

	t.runner.Store(runner)
	response <- pl.PID
	close(response)
	return nil
}

func (t *Terminal) prepareContext(
	callCtx context.Context,
	launch *process.Launch,
) context.Context {
	// Create FrameContext on Terminal's context, inheriting actor/scope from calling context
	pCtx, fc := ctxapi.OpenFrameContextOn(t.ctx, callCtx)

	// Store frame ID, PID, host, and terminal context in FrameContext
	// Pre-allocate pairs slice with exact size
	pairsLen := 4 + len(launch.Context)
	pairs := make([]ctxapi.Pair, pairsLen)
	pairs[0] = ctxapi.Pair{Key: runtime.FrameIDKey, Value: launch.Source}
	pairs[1] = ctxapi.Pair{Key: runtime.FramePIDKey, Value: launch.PID}
	pairs[2] = ctxapi.Pair{Key: runtime.FrameHostKey, Value: t.id}
	pairs[3] = ctxapi.Pair{Key: terminal.TerminalCtxKey, Value: terminal.NewTerminalContext(os.Stdin, os.Stdout, os.Stderr)}

	// Add launch context overrides
	if len(launch.Context) > 0 {
		copy(pairs[4:], launch.Context)
	}

	if err := fc.SetMultiple(pairs...); err != nil {
		t.log.Error("failed to set frame context", zap.Error(err))
	}

	// Store lifecycle hook arrays from Launch
	if len(launch.OnStart) > 0 {
		if err := process.SetOnStartHooks(pCtx, launch.OnStart); err != nil {
			t.log.Error("failed to set onStart hooks", zap.Error(err))
		}
	}

	// Add terminal's cleanup to OnComplete hooks
	onCompleteHooks := launch.OnComplete
	onCompleteHooks = append(onCompleteHooks, func(_ context.Context, pid relay.PID, result *runtime.Result) {
		if result.Error != nil {
			t.log.Error("terminal process execution failed",
				zap.String("pid", pid.String()),
				zap.String("error", result.Error.Error()))
		} else {
			t.log.Info("terminal process execution completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Value.Data()))
		}
		t.cleanup(result)
	})
	if err := process.SetOnCompleteHooks(pCtx, onCompleteHooks); err != nil {
		t.log.Error("failed to set onComplete hooks", zap.Error(err))
	}

	return pCtx
}

//nolint:unparam // ok for now
func (t *Terminal) handleTerminate() error {
	if runner := t.runner.Load(); runner != nil {
		runner.Stop()
	}
	return nil
}

func (t *Terminal) handleSend(msgBatch *relay.Package) error {
	runner := t.runner.Load()
	if runner == nil {
		return process.ErrNoProcess
	}
	return runner.Send(msgBatch)
}

func (t *Terminal) cleanup(_ *runtime.Result) {
	t.logCtrl.RestoreBaseConfig(t.ctx)
	t.runner.Swap(nil)
}

func (t *Terminal) setupLogging() error {
	return t.logCtrl.EnableTemporaryConfig(t.ctx, logsapi.Config{
		MinLevel:            zap.DebugLevel,
		StreamToEvents:      true,
		PropagateDownstream: false,
	})
}

func (t *Terminal) execOp(ctx context.Context, op op) error {
	select {
	case t.opCh <- op:
	default:
		return process.ErrHostBusy
	}

	if op.result == nil {
		return nil
	}

	select {
	case err := <-op.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Attach attaches a process to the terminal.
// This implementation always returns an error as only terminal processes
// can be attached to the terminal host.
// It implements part of the relay.Host interface.
func (t *Terminal) Attach(_ relay.PID, _ chan *relay.Package) (context.CancelFunc, error) {
	return nil, errors.New("only terminal process can be attached to the host")
}

// Detach detaches a process from the terminal.
// This is a no-op in the current implementation.
// It implements part of the relay.Host interface.
func (t *Terminal) Detach(_ relay.PID) {
	// nothing
}

// Send sends a message to the currently running process.
// It implements part of the relay.Host interface.
func (t *Terminal) Send(msg *relay.Package) error {
	// we dont really use pid since we always host a single process
	return t.execOp(t.ctx, op{
		typ:    opSend,
		msg:    msg,
		result: make(chan error, 1),
	})
}

// Launch launches a new process in the terminal.
// It implements part of the process.Host interface.
func (t *Terminal) Launch(ctx context.Context, pl *process.Launch) (relay.PID, error) {
	resp := make(chan relay.PID, 1)
	err := t.execOp(ctx, op{
		ctx:      ctx,
		typ:      opLaunch,
		launch:   pl,
		result:   make(chan error, 1),
		response: resp,
	})
	if err != nil {
		return relay.PID{}, err
	}

	select {
	case newPid := <-resp:
		return newPid, nil
	case <-ctx.Done():
		return relay.PID{}, ctx.Err()
	}
}

// Terminate terminates the currently running process.
// It implements part of the process.Host interface.
func (t *Terminal) Terminate(ctx context.Context, _ relay.PID) error {
	return t.execOp(ctx, op{
		typ:    opTerminate,
		result: make(chan error, 1),
	})
}

// Stop gracefully stops the terminal service.
// It implements the supervisor.Service interface.
func (t *Terminal) Stop(ctx context.Context) error {
	if runner := t.runner.Load(); runner != nil {
		err := t.Send(
			topology.Cancel(
				relay.PID{UniqID: "terminal"},
				runner.pid,
				time.Now().Add(t.config.Lifecycle.StopTimeout),
			),
		)

		if err != nil {
			t.log.Warn("failed to send cancel event", zap.Error(err))
		}

		select {
		case <-runner.Wait():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// UpdateConfig updates the terminal configuration.
// It allows changing configuration parameters without restarting the service.
func (t *Terminal) UpdateConfig(ctx context.Context, cfg *terminal.HostConfig) error {
	return t.execOp(ctx, op{
		typ:    opUpdateConfig,
		cfg:    cfg,
		result: make(chan error, 1),
	})
}
