package pool

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/runtime"
)

// Inline executes function calls synchronously in the caller's goroutine.
// No worker pool, no queuing - calls run to completion immediately.
// Concurrent calls are serialized via mutex to protect the single process.
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
	mu         sync.Mutex
}

// NewInline creates an inline executor.
func NewInline(factory Factory, dispatcher Dispatcher, hooks ...ExecutionHooks) (*Inline, error) {
	proc, err := factory()
	if err != nil {
		return nil, err
	}

	executor := NewExecutor(dispatcher)
	if len(hooks) > 0 {
		executor = executor.WithExecutionHooks(hooks[0])
	}

	return &Inline{
		factory:    factory,
		dispatcher: dispatcher,
		executor:   executor,
		process:    proc,
	}, nil
}

// Call executes synchronously in the caller's goroutine.
// Serializes access to the process via mutex to prevent concurrent access.
func (i *Inline) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.executor.Run(ctx, i.process, method, input), nil
}

// Start is a no-op for inline execution.
func (i *Inline) Start() {}

// Stop closes the process.
func (i *Inline) Stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.process != nil {
		i.process.Close()
		i.process = nil
	}
}
