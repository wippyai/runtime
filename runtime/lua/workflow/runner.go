package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/command"
	"github.com/ponyruntime/pony/runtime/lua/engine/pubsub"
	lua "github.com/yuin/gopher-lua"
)

// WorkflowRunner provides an interface between external systems and the Lua runtime
type WorkflowRunner struct {
	mu       sync.Mutex
	ctx      context.Context
	runner   *engine.Runner
	cmdLayer *command.Layer
	pubLayer *pubsub.Layer
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
	pubLayer *pubsub.Layer,
) *WorkflowRunner {
	return &WorkflowRunner{
		runner:   runner,
		cmdLayer: cmdLayer,
		pubLayer: pubLayer,
	}
}

// Start begins the runner with the specified function and arguments
func (b *WorkflowRunner) Start(ctx context.Context, funcName string, args ...lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("runner already started")
	}

	ctx = b.runner.WithContext(ctx)

	// Start the engine
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

// Step advances execution and returns any commands needing external processing
// Returns:
// - commands that need to be processed
// - whether we are ready to exit
// - any error that occurred
func (b *WorkflowRunner) Step() ([]*command.Command, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil, fmt.Errorf("runner not started")
	}

	var tasks []*engine.Task

	for {
		// Check for completion
		select {
		case result, ok := <-b.exitCh:
			if !ok {
				b.complete = true
				return nil, nil
			}

			if result.Error != nil {
				b.err = result.Error
				b.complete = true
				return nil, result.Error
			}

			if len(result.Result) > 0 {
				b.result = result.Result[0]
			}
			b.complete = true
			return nil, nil
		default:
			// Continue processing
		}

		// Run a step of the engine
		moreTasks, err := b.runner.Step(tasks...)
		if err != nil {
			return nil, fmt.Errorf("step error: %w", err)
		}

		// Get any pending commands after processing
		commands := b.cmdLayer.GetPendingCommands()
		if len(commands) > 0 {
			// We have commands to process externally
			return commands, nil
		}

		// If no more tasks and no commands, we're done with this step
		if len(moreTasks) == 0 && b.runner.GetTaskGroup().GetTaskCount() == 0 {
			return nil, nil
		}

		if len(moreTasks) == 0 && b.runner.GetTaskGroup().GetTaskCount() > 0 {
			moreTasks, err = b.runner.GetTaskGroup().Wait(b.ctx, b.runner.GetCVM(), false)
			if err != nil {
				return nil, err
			}

			if len(moreTasks) == 0 {
				return nil, nil
			}
		}

		for i, _ := range tasks {
			tasks[i] = nil
		}

		tasks = moreTasks
	}
}

// IsComplete returns whether the workflow has completed
func (b *WorkflowRunner) IsComplete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.complete
}

// GetCompletionResult returns the final result and any error
// Should only be called after IsComplete returns true
func (b *WorkflowRunner) GetCompletionResult() (lua.LValue, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.result, b.err
}

// SetCommandResult sets the result for a processed command
func (b *WorkflowRunner) SetCommandResult(cmd *command.Command, result lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.cmdLayer.QueueResult(cmd, result)
	return nil
}

// SetCommandError sets an error for a failed command
func (b *WorkflowRunner) SetCommandError(cmd *command.Command, err error) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.cmdLayer.QueueError(cmd, err)
	return nil
}

// SendValue sends a message via pubsub
func (b *WorkflowRunner) SendValue(topic string, value ...lua.LValue) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return fmt.Errorf("runner not started")
	}

	b.pubLayer.Publish(topic, value...)
	return nil
}

// Stop gracefully shuts down the runner
func (b *WorkflowRunner) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	b.running = false
	return nil
}
