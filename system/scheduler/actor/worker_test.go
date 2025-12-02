package actor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func TestWorkerExecuteSimple(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)
	worker := sched.workers[0]

	// Create a simple processor
	p := &CounterProcess{}
	p.Execute(context.Background(), "", testInput(1))

	proc := &Processor{
		ID:        1,
		Process:   p,
		State:     StateReady,
		scheduler: sched,
	}

	worker.executeOne(proc)

	// Should have stepped once
	if proc.StepCount != 1 {
		t.Fatalf("expected 1 step, got %d", proc.StepCount)
	}
}

func TestWorkerExecuteToCompletion(t *testing.T) {
	var completed atomic.Bool
	var finalResult *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			finalResult = result
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)
	worker := sched.workers[0]

	// Create a simple processor that completes in 2 steps
	p := &CounterProcess{}
	p.Execute(context.Background(), "", testInput(1))

	proc := &Processor{
		ID:        1,
		PID:       testPID(),
		Process:   p,
		State:     StateReady,
		scheduler: sched,
	}

	// First step - yields
	worker.executeOne(proc)

	// Simulate handler completion
	proc.Complete(nil, nil)

	// Second step - completes
	worker.executeOne(proc)

	if !completed.Load() {
		t.Fatal("process should have completed")
	}

	if finalResult.Error != nil {
		t.Fatalf("unexpected error: %v", finalResult.Error)
	}
}

func TestWorkerExecuteWithBlocking(t *testing.T) {
	var completed atomic.Bool

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	sched.Start()
	defer sched.Stop()

	// Use Execute to wait for async handler
	result, err := sched.Execute(context.Background(), testPID(), &SleepProcess{}, "", testInput(10*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("unexpected result error: %v", result.Error)
	}
}

func TestWorkerMetrics(t *testing.T) {
	var wg sync.WaitGroup
	var completed atomic.Int32

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			completed.Add(1)
			wg.Done()
		},
	}
	sched := newTestSchedulerWithLifecycle(2, lc)

	sched.Start()
	defer sched.Stop()

	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
	}

	wg.Wait()

	var totalExecuted uint64
	for _, w := range sched.workers {
		totalExecuted += w.executed.Load()
	}

	// Each process has 2 steps (yield + done)
	if totalExecuted < uint64(n) {
		t.Fatalf("expected at least %d executed, got %d", n, totalExecuted)
	}
}

func TestWorkerUnknownCommand(t *testing.T) {
	var errorReceived error

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			errorReceived = result.Error
		},
	}
	registry := NewRegistry()
	// Don't register any handlers
	sched := NewScheduler(registry, WithWorkers(1), WithLifecycle(lc))
	worker := sched.workers[0]

	p := &CounterProcess{}
	p.Execute(context.Background(), "", testInput(1))

	proc := &Processor{
		ID:        1,
		PID:       testPID(),
		Process:   p,
		State:     StateReady,
		scheduler: sched,
	}

	worker.executeOne(proc)

	if errorReceived == nil {
		t.Fatal("expected error for unknown command")
	}

	if _, ok := errorReceived.(*UnknownCommandError); !ok {
		t.Fatalf("expected UnknownCommandError, got %T", errorReceived)
	}
}

// Benchmarks

func BenchmarkWorkerExecute(b *testing.B) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := &CounterProcess{}
		p.Execute(context.Background(), "", testInput(0))

		proc := &Processor{
			ID:        uint64(i),
			Process:   p,
			State:     StateReady,
			scheduler: sched,
		}

		worker.executeOne(proc)
	}
}
