package terminal

import (
	"context"
	ctxapi "github.com/ponyruntime/pony/api/context"
	logsapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"github.com/ponyruntime/pony/system/logs"
	"go.uber.org/zap"
	"os"
	"sync/atomic"
)

type opType int

const (
	opLaunch opType = iota
	opTerminate
	opSend
	opUpdateConfig
)

// op now carries only the necessary fields.
// For opLaunch we carry a pointer to LaunchProcess, since we expect that structure to be unmodified.
type op struct {
	typ      opType
	ctx      context.Context
	launch   *process.LaunchProcess
	msg      *pubsub.Batch
	cfg      *terminal.HostConfig
	result   chan error
	response chan pubsub.PID
}

type Terminal struct {
	id      registry.ID
	ctx     context.Context
	cfg     *terminal.HostConfig
	opCh    chan op
	done    chan struct{}
	logCtrl *logs.ConfigSwitcher
	log     *zap.Logger
	runner  atomic.Pointer[Runner]
}

func NewTerminal(id registry.ID, cfg *terminal.HostConfig, logCtrl *logs.ConfigSwitcher, log *zap.Logger) *Terminal {
	return &Terminal{
		id:      id,
		cfg:     cfg,
		opCh:    make(chan op, 10),
		done:    make(chan struct{}),
		logCtrl: logCtrl,
		log:     log,
	}
}

func (t *Terminal) Start(ctx context.Context) (<-chan any, error) {
	status := make(chan any, 1)
	go t.run(ctx, status)
	status <- "started"
	return status, nil
}

func (t *Terminal) run(ctx context.Context, status chan<- any) {
	defer close(t.done)
	defer close(status)
	defer t.cleanup(nil) // Ensure logs are restored even on abnormal exit

	// Localize the logger while preserving any values already in ctx.
	t.ctx = context.WithValue(ctx, ctxapi.LoggerCtx, t.log)

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
				t.cfg = op.cfg
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

func (t *Terminal) handleLaunch(ctx context.Context, pl *process.LaunchProcess, response chan pubsub.PID) error {
	if t.runner.Load() != nil {
		return process.ErrHostBusy
	}

	cfg := &RunnerConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	if t.cfg.HideLogs {
		if err := t.setupLogging(); err != nil {
			return err
		}
	}

	// we detach from parent context but carry context handlers
	pCtx := t.ctx

	origComplete := process.GetOnComplete(ctx)
	if origComplete != nil {
		pCtx = process.WithAddedOnComplete(pCtx, origComplete)
	}

	origOnStart := process.GetOnStart(ctx)
	if origOnStart != nil {
		pCtx = process.WithAddedOnStart(pCtx, origOnStart)
	}

	// Set up a new context with the terminal's log configuration.
	pCtx = process.WithAddedOnComplete(pCtx, func(pid pubsub.PID, result *runtime.Result) {
		if result.Error != nil {
			t.log.Error("terminal process execution failed",
				zap.String("pid", pid.String()),
				zap.Error(result.Error))
		} else {
			t.log.Info("terminal process execution completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Payload))
		}
		t.cleanup(result)
	})

	runner, err := NewTerminalRunner(pCtx, cfg, pl)
	if err != nil {
		t.cleanup(nil)
		return err
	}

	t.runner.Store(runner)
	response <- pl.PID
	return nil
}

func (t *Terminal) handleTerminate() error {
	if runner := t.runner.Load(); runner != nil {
		runner.Stop()
	}
	return nil
}

func (t *Terminal) handleSend(msgBatch *pubsub.Batch) error {
	runner := t.runner.Load()
	if runner == nil {
		return process.ErrNoProcess
	}

	return runner.Send(msgBatch)
}

func (t *Terminal) cleanup(result *runtime.Result) {
	// Use the existing service context to restore base log configuration.
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

func (t *Terminal) Send(ctx context.Context, pid pubsub.PID, msg *pubsub.Batch) error {
	return t.execOp(ctx, op{
		typ:    opSend,
		msg:    msg,
		result: make(chan error, 1),
	})
}

func (t *Terminal) Launch(ctx context.Context, pl *process.LaunchProcess) (pubsub.PID, error) {
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

func (t *Terminal) Terminate(ctx context.Context, pid pubsub.PID) error {
	return t.execOp(ctx, op{
		typ:    opTerminate,
		result: make(chan error, 1),
	})
}

func (t *Terminal) Stop(ctx context.Context) error {
	if err := t.Send(ctx, pubsub.PID{}, &pubsub.Batch{&pubsub.Message{Topic: process.TopicCancel}}); err != nil {
		t.log.Warn("failed to send cancel message", zap.Error(err))
	}

	if runner := t.runner.Load(); runner != nil {
		select {
		case <-runner.Wait():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func (t *Terminal) UpdateConfig(ctx context.Context, cfg *terminal.HostConfig) error {
	return t.execOp(ctx, op{
		typ:    opUpdateConfig,
		cfg:    cfg,
		result: make(chan error, 1),
	})
}
