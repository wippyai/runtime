package commands

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
)

// Handler processes a specific type of command
type Handler func(context.Context, *Command) (lua.LValue, error)

// Runner manages command execution and result handling
type Runner struct {
	log      *zap.Logger
	layer    *Layer
	handlers map[CommandType]Handler
	runner   *engine.Runner
	wg       sync.WaitGroup
	mu       sync.Mutex
}

// NewRunner creates a new command runner instance
func NewRunner(
	log *zap.Logger,
	layer *Layer,
	runner *engine.Runner,
	opts ...RunnerOption,
) *Runner {
	r := &Runner{
		log:      log,
		layer:    layer,
		handlers: make(map[CommandType]Handler),
		runner:   runner,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RunnerOption configures the command runner
type RunnerOption func(*Runner)

// WithHandler registers a command handler
func WithHandler(cmdType CommandType, handler Handler) RunnerOption {
	return func(r *Runner) {
		r.handlers[cmdType] = handler
	}
}

// processCommand handles a single command execution
func (r *Runner) processCommand(ctx context.Context, cmd *Command) {
	handler, ok := r.handlers[cmd.cmdType]
	if !ok {
		r.layer.QueueError(cmd, fmt.Errorf("no handler for command type: %s", cmd.cmdType))
		return
	}

	result, err := handler(ctx, cmd)
	if err != nil {
		r.layer.QueueError(cmd, err)
		return
	}

	r.layer.QueueResult(cmd, result)
}

// Process checks for and processes any pending commands
func (r *Runner) Process(ctx context.Context) error {
	r.mu.Lock()
	commands := r.layer.GetPendingCommands()
	r.mu.Unlock()

	if len(commands) == 0 {
		return nil
	}

	// Process commands concurrently
	for _, cmd := range commands {
		cmd := cmd // Capture for goroutine
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.processCommand(ctx, cmd)
			// Wake runner to handle results
			r.runner.GetTaskGroup().WakeUp()
		}()
	}

	return nil
}

// Close waits for all command processing to complete
func (r *Runner) Close(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
