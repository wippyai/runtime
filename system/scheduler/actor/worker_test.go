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
		id:        1,
		Process:   p,
		State:     StateReady,
		scheduler: sched,
	}

	worker.executeOne(proc)

	// After executeOne with sync handler (YieldHandler calls Emit immediately),
	// state should be StateReady (re-queued for next step)
	if proc.State != StateReady {
		t.Fatalf("expected StateReady after sync handler, got %v", proc.State)
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
		id:        1,
		pid:       testPID(),
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
	var finalResult *runtime.Result

	lc := &testLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *runtime.Result) {
			finalResult = result
			completed.Store(true)
		},
	}
	sched := newTestSchedulerWithLifecycle(1, lc)

	sched.Start()
	defer sched.Stop()

	// Use Submit and wait for completion via lifecycle
	_, err := sched.Submit(context.Background(), testPID(), &SleepProcess{}, "", testInput(10*time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for !completed.Load() && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}

	if !completed.Load() {
		t.Fatal("timed out waiting for completion")
	}

	if finalResult.Error != nil {
		t.Fatalf("unexpected result error: %v", finalResult.Error)
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
		id:        1,
		pid:       testPID(),
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
			id:        uint64(i),
			Process:   p,
			State:     StateReady,
			scheduler: sched,
		}

		worker.executeOne(proc)
	}
}
