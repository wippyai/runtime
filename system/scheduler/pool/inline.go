package pool

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Inline executes function calls synchronously in the caller's goroutine.
// No worker pool, no queuing - calls run to completion immediately.
// Concurrent calls are serialized via mutex to protect the single process.
// Implements relay.Receiver for message delivery to running processes.
//
// Use cases:
//   - Eval: Embedding one actor inside another
//   - Testing: Simple synchronous execution
//   - Low-overhead calls where caller is already in a worker goroutine
type Inline struct {
	factory    Factory
	dispatcher Dispatcher
	hooks      ExecutionHooks
	executor   *Executor
	process    process.Process
	mu         sync.Mutex

	// Active execution tracking for message routing
	active sync.Map // map[string]*Executor
}

// NewInline creates an inline executor.
func NewInline(factory Factory, dispatcher Dispatcher, hooks ...ExecutionHooks) (*Inline, error) {
	proc, err := factory()
	if err != nil {
		return nil, err
	}

	var hooksCfg ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	executor := NewExecutor(dispatcher).WithExecutionHooks(hooksCfg)

	return &Inline{
		factory:    factory,
		dispatcher: dispatcher,
		hooks:      hooksCfg,
		executor:   executor,
		process:    proc,
	}, nil
}

// Call executes synchronously in the caller's goroutine.
// Serializes access to the process via mutex to prevent concurrent access.
func (i *Inline) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.process == nil {
		return nil, ErrPoolClosed
	}

	// Get PID from frame context (set by function registry)
	pid, _ := runtime.GetFramePID(ctx)
	i.active.Store(pid.UniqID, i.executor)

	result := i.executor.Run(ctx, i.process, method, input)

	// Unregister - no more lookups will find this executor
	// inbox was already cleared by Run's defer
	i.active.Delete(pid.UniqID)

	// Process is ready for reuse - clearExecution() was called by Step
	// when it returned StepDone
	return result, nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (i *Inline) Send(pkg *relay.Package) error {
	v, ok := i.active.Load(pkg.Target.UniqID)
	if !ok {
		return ErrProcessNotFound
	}
	return v.(*Executor).Send(pkg)
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
