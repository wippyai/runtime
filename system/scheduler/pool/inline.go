package pool

import (
	"context"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// Inline executes calls synchronously in the caller's goroutine.
// Single process, serialized via mutex.
type Inline struct {
	dispatcher dispatcher.Dispatcher
	executor   *Executor
	process    process.Process
	mu         sync.Mutex
	active     sync.Map
}

// NewInline creates an inline executor.
func NewInline(factory process.FactoryFunc, dispatcher dispatcher.Dispatcher, hooks ...ExecutionHooks) (*Inline, error) {
	proc, err := factory()
	if err != nil {
		return nil, err
	}

	var hooksCfg ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	return &Inline{
		dispatcher: dispatcher,
		executor:   NewExecutor(dispatcher).WithExecutionHooks(hooksCfg),
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

	pid, _ := runtime.GetFramePID(ctx)
	i.active.Store(pid.UniqID, i.executor)

	result := i.executor.Run(ctx, i.process, method, input)

	i.active.Delete(pid.UniqID)

	return result, nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (i *Inline) Send(pkg *relay.Package) error {
	v, ok := i.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
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
