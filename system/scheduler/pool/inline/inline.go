// SPDX-License-Identifier: MPL-2.0

package inline

import (
	"context"
	"errors"
	"sync"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler/pool"
)

// Pool executes calls synchronously in the caller's goroutine.
// Single process, serialized via mutex.
type Pool struct {
	dispatcher dispatcher.Dispatcher
	factory    process.FactoryFunc
	process    process.Process
	executor   *pool.Executor
	active     sync.Map
	mu         sync.Mutex
	closed     bool
}

// New creates an inline executor.
func New(factory process.FactoryFunc, d dispatcher.Dispatcher, hooks ...pool.ExecutionHooks) (*Pool, error) {
	proc, err := factory()
	if err != nil {
		return nil, err
	}

	var hooksCfg pool.ExecutionHooks
	if len(hooks) > 0 {
		hooksCfg = hooks[0]
	}

	return &Pool{
		dispatcher: d,
		factory:    factory,
		executor:   pool.NewExecutor(d).WithExecutionHooks(hooksCfg),
		process:    proc,
	}, nil
}

// Call executes synchronously in the caller's goroutine.
// Serializes access to the process via mutex to prevent concurrent access.
func (i *Pool) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.closed {
		return nil, pool.ErrPoolClosed
	}

	if i.process == nil {
		if err := i.replaceProcess(); err != nil {
			return &runtime.Result{Error: err}, nil
		}
	}

	pid, _ := runtime.GetFramePID(ctx)
	i.active.Store(pid.UniqID, i.executor)

	result := i.executor.Run(ctx, i.process, method, input)
	replace := errors.Is(result.Error, process.ErrProcessReplacementRequested)
	if replace {
		result.Error = nil
	}

	i.active.Delete(pid.UniqID)
	i.executor.Reset()
	if replace {
		_ = i.replaceProcess()
	}

	return result, nil
}

func (i *Pool) replaceProcess() error {
	old := i.process
	i.process = nil
	if old != nil {
		old.Close()
	}

	proc, err := i.factory()
	if err != nil {
		return err
	}
	i.process = proc
	return nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (i *Pool) Send(pkg *relay.Package) error {
	return i.SendContext(context.Background(), pkg)
}

// SendContext implements relay.ContextSender. Routes package to target
// execution while honoring cancellation before queue admission.
func (i *Pool) SendContext(ctx context.Context, pkg *relay.Package) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	v, ok := i.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*pool.Executor).SendContext(ctx, pkg)
}

// Start is a no-op for inline execution.
func (i *Pool) Start() {}

// Stop closes the process.
func (i *Pool) Stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	if i.process != nil {
		i.process.Close()
		i.process = nil
	}
}
