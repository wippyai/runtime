package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"io"
	"os"

	"github.com/ponyruntime/pony/api/process"
	"go.uber.org/zap"
)

type RunnerConfig struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func DefaultRunnerConfig() *RunnerConfig {
	return &RunnerConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

type ProcessRunner struct {
	proc   process.Process
	ctx    context.Context
	cancel context.CancelFunc
	log    *zap.Logger
	cfg    *RunnerConfig
}

func NewRunner(
	ctx context.Context,
	cfg *RunnerConfig,
	log *zap.Logger,
	launch process.Launch,
) (*ProcessRunner, error) {
	if cfg == nil {
		cfg = DefaultRunnerConfig()
	}

	pCtx, cancel := context.WithCancel(ctx)
	pCtx = terminal.WithTerminalContext(
		pCtx,
		terminal.NewTerminalContext(cfg.Stdin, cfg.Stdout, cfg.Stderr),
	)

	runner := &ProcessRunner{
		proc:   launch.Process,
		ctx:    pCtx,
		cancel: cancel,
		log:    log,
		cfg:    cfg,
	}

	wrappedOnComplete := append([]process.OnComplete{
		func(pid process.PID, res *runtime.Result) {
			runner.Stop()
		},
	}, launch.OnComplete...)

	if err := launch.Process.Start(pCtx, process.StartProcess{
		PID:        launch.PID,
		Input:      launch.Input,
		OnComplete: wrappedOnComplete,
	}); err != nil {
		cancel()
		return nil, err
	}

	go runner.run(launch.PID)
	return runner, nil
}

func (r *ProcessRunner) run(pid process.PID) {
	defer r.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		if err := r.proc.Step(); err != nil {
			r.log.Error("process execution failed",
				zap.String("pid", pid.String()),
				zap.Error(err),
			)
			r.Stop()
			return
		}
	}
}

func (r *ProcessRunner) Send(msg *process.Message) error {
	return r.proc.Send(msg)
}

func (r *ProcessRunner) Stop() {
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
}

func (r *ProcessRunner) Wait() <-chan struct{} {
	return r.ctx.Done()
}
