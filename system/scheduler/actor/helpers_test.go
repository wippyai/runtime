package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// testExecutor provides synchronous execution semantics for tests.
// It wraps a scheduler and blocks until completion using lifecycle callbacks.
type testExecutor struct {
	sched *Scheduler
	mu    sync.Mutex
	// Track pending operations by PID
	pending map[string]chan *runtime.Result
}

func newTestExecutorWithRegistry(workers int, registry *scheduler.Registry) *testExecutor {
	return newTestExecutorWithRegistryAndOptions(workers, registry)
}

func newTestExecutorWithRegistryAndOptions(workers int, registry *scheduler.Registry, opts ...Option) *testExecutor {
	te := &testExecutor{
		pending: make(map[string]chan *runtime.Result),
	}

	lc := &testLifecycle{
		onComplete: func(_ context.Context, pid relay.PID, result *runtime.Result) {
			te.mu.Lock()
			if ch, ok := te.pending[pid.UniqID]; ok {
				delete(te.pending, pid.UniqID)
				te.mu.Unlock()
				ch <- result
			} else {
				te.mu.Unlock()
			}
		},
	}

	allOpts := []Option{WithWorkers(workers), WithLifecycle(lc)}
	allOpts = append(allOpts, opts...)
	te.sched = NewScheduler(registry, allOpts...)
	return te
}

func newTestExecutorWithOptions(workers int, opts ...Option) *testExecutor {
	registry := scheduler.NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return newTestExecutorWithRegistryAndOptions(workers, registry, opts...)
}

func (te *testExecutor) Start() {
	te.sched.Start()
}

func (te *testExecutor) Stop() {
	te.sched.Stop()
}

func (te *testExecutor) Scheduler() *Scheduler {
	return te.sched
}

func (te *testExecutor) Execute(ctx context.Context, pid relay.PID, p Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	te.mu.Lock()
	te.pending[pid.UniqID] = resultCh
	te.mu.Unlock()

	_, err := te.sched.Submit(ctx, pid, p, method, input)
	if err != nil {
		te.mu.Lock()
		delete(te.pending, pid.UniqID)
		te.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		te.mu.Lock()
		delete(te.pending, pid.UniqID)
		te.mu.Unlock()
		return nil, ctx.Err()
	}
}

func waitForCompletionInt64(completed *atomic.Int64, expected int64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for completed.Load() < expected && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}
}
