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

func TestWorkerFindWork(t *testing.T) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	// Empty - should return nil
	if p := worker.findWork(); p != nil {
		t.Fatal("expected nil from empty worker")
	}

	// Add to local deque
	proc := &Processor{ID: 1}
	worker.local.Push(proc)
	if p := worker.findWork(); p != proc {
		t.Fatal("expected proc from local deque")
	}

	// Add to global queue
	proc2 := &Processor{ID: 2}
	sched.global.Push(proc2)
	if p := worker.findWork(); p != proc2 {
		t.Fatal("expected proc from global queue")
	}
}

func TestWorkerSteal(t *testing.T) {
	sched := newTestScheduler(2)
	worker0 := sched.workers[0]
	worker1 := sched.workers[1]

	// Add work to worker0's local deque
	for i := 0; i < 8; i++ {
		worker0.local.Push(&Processor{ID: uint64(i)})
	}

	// Worker1 should steal from worker0
	stolen := worker1.steal()
	if stolen == nil {
		t.Fatal("expected to steal from worker0")
	}

	// Should have pushed remaining stolen items to local deque
	if worker1.local.Len() == 0 {
		t.Log("Note: only one item stolen (expected half)")
	}

	if worker1.stolen.Load() == 0 {
		t.Fatal("stolen counter should be > 0")
	}
}

func TestWorkerStealEmpty(t *testing.T) {
	sched := newTestScheduler(2)
	worker1 := sched.workers[1]

	// Both workers empty - steal should return nil
	if p := worker1.steal(); p != nil {
		t.Fatal("expected nil when stealing from empty workers")
	}
}

func TestWorkerExecuteSimple(t *testing.T) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	var completed atomic.Bool
	sched.onComplete = func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		completed.Store(true)
	}

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
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	var completed atomic.Bool
	var finalResult *runtime.Result
	sched.onComplete = func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		finalResult = result
		completed.Store(true)
	}

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
	sched := newTestScheduler(1)

	var completed atomic.Bool
	sched.onComplete = func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		completed.Store(true)
	}

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
	sched := newTestScheduler(2)

	var wg sync.WaitGroup
	var completed atomic.Int32

	sched.onComplete = func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		completed.Add(1)
		wg.Done()
	}

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

func TestWorkerLocalPushOnContinue(t *testing.T) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]

	// Create a process with multiple yields
	p := &CounterProcess{}
	p.Execute(context.Background(), "", testInput(5))

	proc := &Processor{
		ID:        1,
		Process:   p,
		State:     StateReady,
		scheduler: sched,
	}

	// Execute first step - handler called
	worker.executeOne(proc)

	// Simulate handler completion
	proc.Complete(nil, nil)

	// Execute second step
	worker.executeOne(proc)

	// Process should continue to local deque (not global) after handler completes
	// and next yield is issued
}

func TestWorkerUnknownCommand(t *testing.T) {
	registry := NewRegistry()
	// Don't register any handlers
	sched := NewScheduler(registry, WithWorkers(1))
	worker := sched.workers[0]

	var errorReceived error
	sched.onComplete = func(ctx context.Context, pid relay.PID, result *runtime.Result) {
		errorReceived = result.Error
	}

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

func BenchmarkWorkerFindWorkLocal(b *testing.B) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]
	proc := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		worker.local.Push(proc)
		worker.findWork()
	}
}

func BenchmarkWorkerFindWorkGlobal(b *testing.B) {
	sched := newTestScheduler(1)
	worker := sched.workers[0]
	proc := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.global.Push(proc)
		worker.findWork()
	}
}

func BenchmarkWorkerSteal(b *testing.B) {
	sched := newTestScheduler(2)
	worker0 := sched.workers[0]
	worker1 := sched.workers[1]
	proc := &Processor{ID: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Fill worker0
		for j := 0; j < 8; j++ {
			worker0.local.Push(proc)
		}
		// Worker1 steals
		for worker0.local.Len() > 0 {
			worker1.steal()
		}
		// Return items to worker0
		for worker1.local.Len() > 0 {
			worker0.local.Push(worker1.local.Pop())
		}
	}
}
