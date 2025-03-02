package terminal

import (
	"context"
	"errors"
	ctxapi "github.com/ponyruntime/pony/api/context"
	logsapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/system/logs"
	"go.uber.org/zap"
	"os"
	"sync/atomic"
	"time"
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
	msg      *pubsub.Package
	cfg      *terminal.HostConfig
	result   chan error
	response chan pubsub.PID
	// For attach operation
	attachCh chan *pubsub.Package
	detach   chan context.CancelFunc
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

	t.ctx = logsapi.WithLogger(pubsub.WithHost(ctx, t), t.log)

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

func (t *Terminal) handleLaunch(ctx context.Context, pl *process.Launch, response chan pubsub.PID) error {
	if t.runner.Load() != nil {
		close(response)
		return process.ErrHostBusy
	}

	cfg := &RunnerConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	if t.config.HideLogs {
		if err := t.setupLogging(); err != nil {
			close(response)
			return err
		}
	}

	rCtx := terminal.WithTerminalContext(
		t.prepareContext(ctx, pl.PID, pl.Lifecycle),
		terminal.NewTerminalContext(cfg.Stdin, cfg.Stdout, cfg.Stderr),
	)

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
	ctx context.Context,
	pid pubsub.PID,
	lifecycle process.Lifecycle,
) context.Context {
	pCtx := ctxapi.MergeContext(t.ctx, ctx)

	// global lifecycle
	pCtx = process.GetProcesses(ctx).AttachLifecycle(ctx, lifecycle)

	// service lifecycle
	pCtx = process.WithAddedOnComplete(pCtx, func(pid pubsub.PID, result *runtime.Result) {
		if result.Error != nil {
			t.log.Error("terminal process execution failed",
				zap.String("pid", pid.String()),
				zap.Error(result.Error))
		} else {
			t.log.Info("terminal process execution completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Payload.Data()))
		}
		t.cleanup(result)
	})

	pCtx = pubsub.WithHost(pCtx, t)
	pCtx = logsapi.WithLogger(pCtx, t.log.Named(pid.UniqID))

	return pCtx
}

func (t *Terminal) handleTerminate() error {
	if runner := t.runner.Load(); runner != nil {
		runner.Stop()
	}
	return nil
}

func (t *Terminal) handleSend(msgBatch *pubsub.Package) error {
	runner := t.runner.Load()
	if runner == nil {
		return process.ErrNoProcess
	}
	return runner.Send(msgBatch)
}

func (t *Terminal) cleanup(result *runtime.Result) {
	t.logCtrl.RestoreBaseConfig(context.Background())
	if runner := t.runner.Swap(nil); runner != nil {
		runner.Stop()
	}
}

func (t *Terminal) setupLogging() error {
	return t.logCtrl.EnableTemporaryConfig(t.ctx, logsapi.Config{
		MinLevel:       zap.DebugLevel,
		StreamToEvents: true,
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
// It implements part of the pubsub.Host interface.
func (t *Terminal) Attach(pid pubsub.PID, ch chan *pubsub.Package) (context.CancelFunc, error) {
	return nil, errors.New("only terminal process can be attached to the host")
}

// Detach detaches a process from the terminal.
// This is a no-op in the current implementation.
// It implements part of the pubsub.Host interface.
func (t *Terminal) Detach(pid pubsub.PID) {
	// nothing
}

// Send sends a message to the currently running process.
// It implements part of the pubsub.Host interface.
func (t *Terminal) Send(msg *pubsub.Package) error {
	// we dont really use pid since we always host a single process
	return t.execOp(t.ctx, op{
		typ:    opSend,
		msg:    msg,
		result: make(chan error, 1),
	})
}

// Launch launches a new process in the terminal.
// It implements part of the process.Host interface.
func (t *Terminal) Launch(ctx context.Context, pl *process.Launch) (pubsub.PID, error) {
	resp := make(chan pubsub.PID, 1)
	err := t.execOp(ctx, op{
		ctx:      ctx,
		typ:      opLaunch,
		launch:   pl,
		result:   make(chan error, 1),
		response: resp,
	})
	if err != nil {
		return pubsub.PID{}, err
	}

	select {
	case newPid := <-resp:
		return newPid, nil
	case <-ctx.Done():
		return pubsub.PID{}, ctx.Err()
	}
}

// Terminate terminates the currently running process.
// It implements part of the process.Host interface.
func (t *Terminal) Terminate(ctx context.Context, pid pubsub.PID) error {
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
				pubsub.PID{ID: t.id},
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
