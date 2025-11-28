package pool

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/runtime"
)

// Inline executes function calls synchronously in the caller's goroutine.
// No worker pool, no queuing - calls run to completion immediately.
//
// Use cases:
//   - Eval: Embedding one actor inside another
//   - Testing: Simple synchronous execution
//   - Low-overhead calls where caller is already in a worker goroutine
type Inline struct {
	factory    Factory
	dispatcher Dispatcher
	executor   *Executor
	process    process2.Process
}

// NewInline creates an inline executor.
func NewInline(factory Factory, dispatcher Dispatcher) (*Inline, error) {
	proc, err := factory()
	if err != nil {
		return nil, err
	}

	return &Inline{
		factory:    factory,
		dispatcher: dispatcher,
		executor:   NewExecutor(dispatcher),
		process:    proc,
	}, nil
}

// Call executes synchronously in the caller's goroutine.
func (i *Inline) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	return i.executor.Run(ctx, i.process, method, input), nil
}

// Start is a no-op for inline execution.
func (i *Inline) Start() {}

// Stop closes the process.
func (i *Inline) Stop() {
	if i.process != nil {
		i.process.Close()
	}
}
