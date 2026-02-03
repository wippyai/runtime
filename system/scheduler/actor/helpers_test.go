package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
)

// testExecutor provides synchronous execution semantics for tests.
// It wraps a scheduler and blocks until completion using lifecycle callbacks.
type testExecutor struct {
	sched   *Scheduler
	pending map[string]chan *runtime.Result
	mu      sync.Mutex
}

func newTestExecutorWithRegistry(workers int, registry *scheduler.Registry) *testExecutor {
	return newTestExecutorWithRegistryAndOptions(workers, registry)
}

func newTestExecutorWithRegistryAndOptions(workers int, registry *scheduler.Registry, opts ...Option) *testExecutor {
	te := &testExecutor{
		pending: make(map[string]chan *runtime.Result),
	}

	lc := &testLifecycle{
		onComplete: func(_ context.Context, p pid.PID, result *runtime.Result) {
			te.mu.Lock()
			if ch, ok := te.pending[p.UniqID]; ok {
				delete(te.pending, p.UniqID)
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

func (te *testExecutor) Start() {
	te.sched.Start()
}

func (te *testExecutor) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	te.sched.Stop(ctx)
}

// testStopContext returns a context with a short timeout for tests.
func testStopContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 500*time.Millisecond)
}

// testStopScheduler stops the scheduler with a short timeout.
func testStopScheduler(sched *Scheduler) {
	ctx, cancel := testStopContext()
	defer cancel()
	sched.Stop(ctx)
}

func (te *testExecutor) Scheduler() *Scheduler {
	return te.sched
}

func (te *testExecutor) Execute(ctx context.Context, p pid.PID, proc process.Process, method string, input payload.Payloads) (*runtime.Result, error) {
	resultCh := make(chan *runtime.Result, 1)

	te.mu.Lock()
	te.pending[p.UniqID] = resultCh
	te.mu.Unlock()

	_, err := te.sched.Submit(ctx, p, proc, method, input)
	if err != nil {
		te.mu.Lock()
		delete(te.pending, p.UniqID)
		te.mu.Unlock()
		return nil, err
	}

	select {
	case result := <-resultCh:
		return result, nil
	case <-ctx.Done():
		te.mu.Lock()
		delete(te.pending, p.UniqID)
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
