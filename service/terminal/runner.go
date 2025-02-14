package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/service/terminal"
	"io"
	"os"
	"sync"

	"github.com/ponyruntime/pony/api/process"
	"go.uber.org/zap"
)

// RunnerConfig holds the configuration for a Runner.
type RunnerConfig struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// DefaultRunnerConfig returns the default RunnerConfig.
func DefaultRunnerConfig() *RunnerConfig {
	return &RunnerConfig{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
}

// Runner manages the lifecycle of a process.
type Runner struct {
	proc   process.Process
	ctx    context.Context
	cancel context.CancelFunc
	log    *zap.Logger
	cfg    *RunnerConfig
	once   sync.Once
}

// NewTerminalRunner creates a new Runner. It derives a child context from the provided
// terminal context (which might already include an onComplete callback) and attaches the terminal values.
func NewTerminalRunner(
	ctx context.Context,
	cfg *RunnerConfig,
	log *zap.Logger,
	launch *process.LaunchProcess,
) (*Runner, error) {
	if cfg == nil {
		cfg = DefaultRunnerConfig()
	}

	// Derive a runner context from the provided terminal context.
	runnerCtx, cancel := context.WithCancel(ctx)
	// Attach terminal IO context values (these keys are separate and do not override onComplete)
	runnerCtx = terminal.WithTerminalContext(
		runnerCtx,
		terminal.NewTerminalContext(cfg.Stdin, cfg.Stdout, cfg.Stderr),
	)

	runnerCtx = process.WithOnComplete(runnerCtx, func(pid process.PID, result *runtime.Result) {
		cancel()
	})

	runner := &Runner{
		proc:   launch.Process,
		ctx:    runnerCtx,
		cancel: cancel,
		log:    log,
		cfg:    cfg,
	}

	// Start the process with the runner's context.
	if err := runner.proc.Start(runnerCtx, launch.PID, launch.Input); err != nil {
		cancel()
		return nil, err
	}

	// Launch the runner loop.
	go runner.run(launch.PID)
	return runner, nil
}

func (r *Runner) run(pid process.PID) {
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

func (r *Runner) Send(msg ...*process.Message) error {
	return r.proc.Send(msg...)
}

func (r *Runner) Stop() {
	r.once.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
	})
}

func (r *Runner) Wait() <-chan struct{} {
	return r.ctx.Done()
}
