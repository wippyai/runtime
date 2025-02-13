package terminal

import (
	"context"
	logsapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	logs2 "github.com/ponyruntime/pony/system/logs"
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

type op struct {
	typ        opType
	pid        process.PID
	proc       process.Process
	input      payload.Payloads
	msg        *process.Message
	cfg        *terminal.HostConfig
	result     chan error
	response   chan process.PID
	onComplete []process.OnComplete
}

type Terminal struct {
	id      registry.ID
	ctx     context.Context
	cfg     *terminal.HostConfig
	opCh    chan op
	done    chan struct{}
	logCtrl *logs2.ConfigSwitcher
	log     *zap.Logger
	runner  atomic.Pointer[ProcessRunner]
}

func NewTerminal(id registry.ID, cfg *terminal.HostConfig, logCtrl *logs2.ConfigSwitcher, log *zap.Logger) *Terminal {
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

	t.ctx = ctx

	for {
		select {
		case <-ctx.Done():
			return
		case op := <-t.opCh:
			var err error

			switch op.typ {
			case opLaunch:
				err = t.handleLaunch(op.pid, op.proc, op.input, op.response, op.onComplete)
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

func (t *Terminal) handleLaunch(pid process.PID, proc process.Process, input payload.Payloads, response chan process.PID, parentOnComplete []process.OnComplete) error {
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

	// Merge parent's onComplete callbacks with terminal's own callback.
	mergedOnComplete := make([]process.OnComplete, 0, len(parentOnComplete)+1)
	mergedOnComplete = append(mergedOnComplete, parentOnComplete...)
	mergedOnComplete = append(mergedOnComplete, func(pid process.PID, result *runtime.Result) {
		t.log.Info("terminal process execution completed",
			zap.String("pid", pid.String()),
			zap.Any("result", result),
		)
		t.cleanup(result)
	})

	runner, err := NewRunner(t.ctx, cfg, t.log, process.Launch{
		PID:        pid,
		Process:    proc,
		Input:      input,
		OnComplete: mergedOnComplete,
	})
	if err != nil {
		t.cleanup(nil)
		return err
	}

	t.runner.Store(runner)
	// Send back the new PID as the response
	response <- pid
	return nil
}

func (t *Terminal) handleTerminate() error {
	if runner := t.runner.Load(); runner != nil {
		runner.Stop()
	}
	return nil
}

func (t *Terminal) handleSend(msg *process.Message) error {
	runner := t.runner.Load()
	if runner == nil {
		return process.ErrNoProcess
	}
	return runner.Send(msg)
}

func (t *Terminal) cleanup(result *runtime.Result) {
	// Always restore logging config first
	t.logCtrl.RestoreBaseConfig(t.ctx)
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

func (t *Terminal) Send(ctx context.Context, pid process.PID, msg *process.Message) error {
	return t.execOp(ctx, op{
		typ:    opSend,
		msg:    msg,
		result: make(chan error, 1),
	})
}

func (t *Terminal) Launch(ctx context.Context, pl process.Launch) (process.PID, error) {
	resp := make(chan process.PID, 1)
	err := t.execOp(ctx, op{
		typ:        opLaunch,
		pid:        pl.PID,
		proc:       pl.Process,
		input:      pl.Input,
		result:     make(chan error, 1),
		response:   resp,
		onComplete: pl.OnComplete,
	})
	if err != nil {
		return process.PID{}, err
	}

	select {
	case newPid := <-resp:
		return newPid, nil
	case <-ctx.Done():
		return process.PID{}, ctx.Err()
	}
}

func (t *Terminal) Terminate(ctx context.Context, pid process.PID) error {
	return t.execOp(ctx, op{
		typ:    opTerminate,
		result: make(chan error, 1),
	})
}

func (t *Terminal) Stop(ctx context.Context) error {
	if err := t.Send(ctx, process.PID{}, &process.Message{Topic: process.TopicCancel}); err != nil {
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
