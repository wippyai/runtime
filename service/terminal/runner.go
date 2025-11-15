package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
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
	pid    relay.PID
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
	launch *process.Launch,
) (*Runner, error) {
	if cfg == nil {
		cfg = DefaultRunnerConfig()
	}

	// Derive a runner context from the provided terminal context.
	runnerCtx, cancel := context.WithCancel(ctx)

	if err := process.SetOnComplete(runnerCtx, func(_ relay.PID, _ *runtime.Result) {
		cancel()
	}); err != nil {
		return nil, fmt.Errorf("failed to set onComplete callback: %w", err)
	}

	runner := &Runner{
		pid:    launch.PID,
		proc:   launch.Process,
		ctx:    runnerCtx,
		cancel: cancel,
		cfg:    cfg,
	}

	// Serve the process with the runner's context.
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
			return
		}
	}
}

// Send forwards a package to the underlying process.
// Returns an error if the process cannot receive the package.
func (r *Runner) Send(pkg *relay.Package) error {
	return r.proc.Send(pkg)
}

// Stop gracefully terminates the runner and its associated process.
// This method is idempotent and can be called multiple times safely.
func (r *Runner) Stop() {
	r.once.Do(func() {
		r.proc.Terminate()
		if r.cancel != nil {
			r.cancel()
		}
	})
}

// Wait returns a channel that will be closed when the runner terminates.
// This can be used to wait for the runner to complete its execution.
func (r *Runner) Wait() <-chan struct{} {
	return r.ctx.Done()
}
