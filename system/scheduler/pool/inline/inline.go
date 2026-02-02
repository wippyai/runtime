package inline

import (
	"context"
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
	process    process.Process
	executor   *pool.Executor
	active     sync.Map
	mu         sync.Mutex
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
		executor:   pool.NewExecutor(d).WithExecutionHooks(hooksCfg),
		process:    proc,
	}, nil
}

// Call executes synchronously in the caller's goroutine.
// Serializes access to the process via mutex to prevent concurrent access.
func (i *Pool) Call(ctx context.Context, method string, input payload.Payloads) (*runtime.Result, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.process == nil {
		return nil, pool.ErrPoolClosed
	}

	pid, _ := runtime.GetFramePID(ctx)
	i.active.Store(pid.UniqID, i.executor)

	result := i.executor.Run(ctx, i.process, method, input)

	i.active.Delete(pid.UniqID)
	i.executor.Reset()

	return result, nil
}

// Send implements relay.Receiver. Routes package to target execution.
func (i *Pool) Send(pkg *relay.Package) error {
	v, ok := i.active.Load(pkg.Target.UniqID)
	if !ok {
		return process.ErrProcessNotFound
	}
	return v.(*pool.Executor).Send(pkg)
}

// Start is a no-op for inline execution.
func (i *Pool) Start() {}

// Stop closes the process.
func (i *Pool) Stop() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.process != nil {
		i.process.Close()
		i.process = nil
	}
}
