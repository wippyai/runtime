package command_layer1

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
)

// Runner provides an interface between external systems and the Lua runtime
type Runner struct {
	mu       sync.Mutex
	ctx      context.Context
	runner   *engine.Runner
	cmdLayer *command.Layer
	pubLayer *subscribe.Layer
	running  bool
	exitCh   <-chan engine.Result
	complete bool
	result   lua.LValue
	err      error
}

// NewWorkflowRunner creates a new bridge runner instance
func NewWorkflowRunner(
	runner *engine.Runner,
	cmdLayer *command.Layer,
	pubLayer *subscribe.Layer,
) *Runner {
	return &Runner{
		runner:   runner,
		cmdLayer: cmdLayer,
		pubLayer: pubLayer,
	}
}

// Start begins the runner with the specified function and arguments
func (b *Runner) Start(ctx context.Context, funcName string, args ...lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("runner already started")
	}

	ctx = b.runner.WithContext(ctx)

	// Launch the engine
	exitCh, err := b.runner.Start(ctx, funcName, args...)
	if err != nil {
		return fmt.Errorf("failed to start runner: %w", err)
	}

	b.ctx = ctx
	b.exitCh = exitCh
	b.running = true
	b.complete = false
	b.result = nil
	b.err = nil
	return nil
}

// checkCompletion checks if workflow has completed and updates state accordingly
func (b *Runner) checkCompletion() (bool, error) {
	select {
	case result, ok := <-b.exitCh:
		if !ok {
			b.complete = true
			return true, nil
		}

		if result.Error != nil {
			b.err = result.Error
			b.complete = true
			return true, result.Error
		}

		if len(result.Result) > 0 {
			b.result = result.Result[0]
		}
		b.complete = true
		return true, nil
	default:
		return false, nil
	}
}

// processMoreTasks gets next batch of tasks if available
func (b *Runner) processMoreTasks(tasks []*engine.Task) ([]*engine.Task, error) {
	if len(tasks) == 0 && b.runner.GetTaskGroup().GetTaskCount() > 0 {
		moreTasks, err := b.runner.GetTaskGroup().Wait(b.ctx, b.runner.GetCVM(), false)
		if err != nil {
			return nil, err
		}
		return moreTasks, nil
	}
	return tasks, nil
}

// Step advances execution and returns any commands needing external processing
// Returns:
// - commands that need to be processed
// - whether we are ready to exit
// - any error that occurred
func (b *Runner) Step() ([]*command.Command, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil, fmt.Errorf("runner not started")
	}

	var tasks []*engine.Task

	for {
		// Run a step of the engine
		moreTasks, err := b.runner.Step(tasks...)
		if err != nil {
			return nil, fmt.Errorf("step error: %w", err)
		}

		if completed, err := b.checkCompletion(); completed {
			return nil, err
		}

		// Check for pending commands
		if commands := b.cmdLayer.GetPendingCommands(); len(commands) > 0 {
			return commands, nil
		}

		// If no more tasks and no commands, check completion one more time
		if len(moreTasks) == 0 && b.runner.GetTaskGroup().GetTaskCount() == 0 {
			return nil, nil
		}

		// Process more tasks if available
		moreTasks, err = b.processMoreTasks(moreTasks)
		if err != nil {
			return nil, err
		}

		// No tasks available, exit step
		if len(moreTasks) == 0 {
			return nil, nil
		}

		// Clear and update task list
		for i := range tasks {
			tasks[i] = nil
		}
		tasks = moreTasks
	}
}

// IsComplete returns whether the workflow has completed
func (b *Runner) IsComplete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.complete
}

// GetCompletionResult returns the final result and any error
// Should only be called after IsComplete returns true
func (b *Runner) GetCompletionResult() (lua.LValue, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.result, b.err
}

// SendResult sets the result for a processed command
func (b *Runner) SendResult(cmd *command.Command, result lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.cmdLayer.QueueResult(cmd, result)
	return nil
}

// SendError sets an error for a failed command
func (b *Runner) SendError(cmd *command.Command, err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.cmdLayer.QueueError(cmd, err)
	return nil
}

// SendValue sends a message via pubsub, values will be send as individual arguments.
func (b *Runner) SendValue(topic string, value ...lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.pubLayer.Publish(topic, value...)
	return nil
}

// Stop gracefully shuts down the runner
func (b *Runner) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		b.running = false
	}
}
