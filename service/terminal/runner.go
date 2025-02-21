package terminal

import (
	"context"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"io"
	"os"
	"sync"
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
	pid    pubsub.PID
	proc   process.Process
	ctx    context.Context
	cancel context.CancelFunc
	cfg    *RunnerConfig
	once   sync.Once
}

// NewTerminalRunner creates a new Runner. It derives a child context from the provided
// terminal context (which might already include an onComplete callback) and attaches the terminal values.
func NewTerminalRunner(
	ctx context.Context,
	cfg *RunnerConfig,
	launch *process.LaunchProcess,
) (*Runner, error) {
	if cfg == nil {
		cfg = DefaultRunnerConfig()
	}

	// Derive a runner context from the provided terminal context.
	runnerCtx, cancel := context.WithCancel(ctx)
	runnerCtx = process.WithAddedOnComplete(runnerCtx, func(pid pubsub.PID, result *runtime.Result) {
		cancel()
	})

	runner := &Runner{
		pid:    launch.PID,
		proc:   launch.Process,
		ctx:    runnerCtx,
		cancel: cancel,
		cfg:    cfg,
	}

	// Start the process with the runner's context.
	if err := runner.proc.Start(runnerCtx, launch.PID, launch.Input); err != nil {
		cancel()
		return nil, err
	}

	// Launch the runner loop.
	go runner.run()

	return runner, nil
}

func (r *Runner) run() {
	defer r.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		_, err := r.proc.Step()
		if err != nil {
			r.Stop()
			return
		}
	}
}

func (r *Runner) Send(pkg *pubsub.Package) error {
	return r.proc.Send(pkg)
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
