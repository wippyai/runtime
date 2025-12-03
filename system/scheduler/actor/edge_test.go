package actor

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
)

// edgeTestLifecycle implements process.Lifecycle for edge tests
type edgeTestLifecycle struct {
	onComplete func(context.Context, relay.PID, *apiruntime.Result)
}

func (l *edgeTestLifecycle) OnStart(ctx context.Context, pid relay.PID, proc Process) {}

func (l *edgeTestLifecycle) OnComplete(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
	if l.onComplete != nil {
		l.onComplete(ctx, pid, result)
	}
}

func newEdgeTestScheduler(workers int, lc *edgeTestLifecycle) *Scheduler {
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	registry.Register(CmdSleep, SleepHandler())
	return NewScheduler(registry, WithWorkers(workers), WithLifecycle(lc))
}

// Edge case: Single worker
func TestSingleWorker(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(1, lc)

	sched.Start()
	defer sched.Stop()

	const n = 100
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(5))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}
}

// Edge case: Queue overflow - submit more than queue capacity
func TestQueueOverflow(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	// Small queue size
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	sched := NewScheduler(registry, WithWorkers(2), WithQueueSize(16), WithLifecycle(lc))

	sched.Start()
	defer sched.Stop()

	// Submit 10x the queue size
	const n = 160
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
	}

	deadline := time.Now().Add(10 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}
}

// Edge case: Burst submission at queue boundary
func TestQueueBoundary(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	queueSize := 64
	registry := NewRegistry()
	registry.Register(CmdComplete, CompleteHandler())
	registry.Register(CmdYield, YieldHandler())
	sched := NewScheduler(registry, WithWorkers(4), WithQueueSize(queueSize), WithLifecycle(lc))

	sched.Start()
	defer sched.Stop()

	// Submit exactly queue size, then queue size again
	for batch := 0; batch < 3; batch++ {
		for i := 0; i < queueSize; i++ {
			sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
		}
		// Let some complete
		time.Sleep(10 * time.Millisecond)
	}

	expected := int32(queueSize * 3)
	deadline := time.Now().Add(10 * time.Second)
	for completed.Load() < expected && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != expected {
		t.Fatalf("expected %d completed, got %d", expected, completed.Load())
	}
}

// Edge case: Many workers, few processes
func TestManyWorkersFewerProcesses(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(32, lc)

	sched.Start()
	defer sched.Stop()

	// Only 5 processes for 32 workers
	const n = 5
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(10))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}
}

// Edge case: Massive parallel submission
func TestMassiveParallelSubmission(t *testing.T) {
	var completed atomic.Int64

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(runtime.GOMAXPROCS(0), lc)

	sched.Start()
	defer sched.Stop()

	const n = 10000
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < n/goroutines; i++ {
				sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(1))
			}
		}()
	}

	wg.Wait()

	deadline := time.Now().Add(30 * time.Second)
	for completed.Load() < int64(n) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	if completed.Load() != int64(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}
}

// Edge case: Execute blocking with various worker counts
func TestExecuteWithSingleWorker(t *testing.T) {
	te := newTestExecutor(1)
	te.Start()
	defer te.Stop()

	result, err := te.Execute(context.Background(), testPID(), &CounterProcess{}, "", testInput(5))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

// Edge case: Concurrent Execute calls
func TestConcurrentExecute(t *testing.T) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	const n = 100
	var wg sync.WaitGroup
	var errors atomic.Int32

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(id int) {
			defer wg.Done()
			pid := relay.PID{UniqID: fmt.Sprintf("test-%d", id)}
			result, err := te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput(3))
			if err != nil || result == nil {
				errors.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if errors.Load() > 0 {
		t.Fatalf("%d executes failed", errors.Load())
	}
}

// Edge case: Work distribution under imbalanced load
func TestWorkDistributionImbalanced(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(4, lc)

	sched.Start()
	defer sched.Stop()

	// Submit batch of work
	const n = 100
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(5))
	}

	deadline := time.Now().Add(10 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}

	// Verify work was distributed across workers
	stats := sched.WorkerStats()
	nonZeroWorkers := 0
	for _, s := range stats {
		if s["executed"] > 0 {
			nonZeroWorkers++
		}
	}

	// Multiple workers should have processed work
	if nonZeroWorkers < 2 {
		t.Logf("Warning: only %d workers executed tasks", nonZeroWorkers)
	}
}

// Benchmark: Execute throughput (blocking call)
func BenchmarkExecuteThroughput(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

// Benchmark: Execute parallel throughput
func BenchmarkExecuteParallelThroughput(b *testing.B) {
	te := newTestExecutor(runtime.GOMAXPROCS(0))
	te.Start()
	defer te.Stop()

	input := testInput(10)
	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			i := counter.Add(1)
			pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
			te.Execute(ctx, pid, &CounterProcess{}, "", input)
		}
	})
}

// Benchmark: Worker count scaling
func BenchmarkWorkerScaling1(b *testing.B) {
	benchmarkWithWorkers(b, 1)
}

func BenchmarkWorkerScaling2(b *testing.B) {
	benchmarkWithWorkers(b, 2)
}

func BenchmarkWorkerScaling4(b *testing.B) {
	benchmarkWithWorkers(b, 4)
}

func BenchmarkWorkerScaling8(b *testing.B) {
	benchmarkWithWorkers(b, 8)
}

func BenchmarkWorkerScaling16(b *testing.B) {
	benchmarkWithWorkers(b, 16)
}

func BenchmarkWorkerScaling32(b *testing.B) {
	benchmarkWithWorkers(b, 32)
}

func benchmarkWithWorkers(b *testing.B, workers int) {
	te := newTestExecutor(workers)
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

// Benchmark: Queue size impact
func BenchmarkQueueSize16(b *testing.B) {
	benchmarkWithQueueSize(b, 16)
}

func BenchmarkQueueSize64(b *testing.B) {
	benchmarkWithQueueSize(b, 64)
}

func BenchmarkQueueSize256(b *testing.B) {
	benchmarkWithQueueSize(b, 256)
}

func BenchmarkQueueSize1024(b *testing.B) {
	benchmarkWithQueueSize(b, 1024)
}

func benchmarkWithQueueSize(b *testing.B, queueSize int) {
	te := newTestExecutorWithOptions(4, WithQueueSize(queueSize))
	te.Start()
	defer te.Stop()

	ctx := context.Background()
	input := testInput(10)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("bench-%d", i)}
		te.Execute(ctx, pid, &CounterProcess{}, "", input)
	}
}

// Stress test: Rapid start/stop cycles
func TestRapidStartStop(t *testing.T) {
	for i := 0; i < 50; i++ {
		var completed atomic.Int32
		lc := &edgeTestLifecycle{
			onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
				completed.Add(1)
			},
		}
		sched := newEdgeTestScheduler(4, lc)
		sched.Start()

		// Submit some work
		for j := 0; j < 10; j++ {
			sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(2))
		}

		// Wait briefly then stop
		deadline := time.Now().Add(time.Second)
		for completed.Load() < 10 && time.Now().Before(deadline) {
			runtime.Gosched()
		}

		sched.Stop()

		if completed.Load() != 10 {
			t.Fatalf("cycle %d: expected 10 completed, got %d", i, completed.Load())
		}
	}
}

// Stress test: Burst load after idle period
func TestBurstAfterIdle(t *testing.T) {
	var completed atomic.Int64

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(4, lc)
	sched.Start()
	defer sched.Stop()

	for burst := 0; burst < 10; burst++ {
		// Let workers go idle
		time.Sleep(50 * time.Millisecond)

		// Burst of work
		startCount := completed.Load()
		for i := 0; i < 100; i++ {
			sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(2))
		}

		// Wait for burst to complete
		deadline := time.Now().Add(5 * time.Second)
		for completed.Load() < startCount+100 && time.Now().Before(deadline) {
			runtime.Gosched()
		}

		if completed.Load() < startCount+100 {
			t.Fatalf("burst %d: expected %d completed, got %d", burst, startCount+100, completed.Load())
		}
	}
}

// Stress test: Single worker under sustained load
func TestSingleWorkerSustained(t *testing.T) {
	var completed atomic.Int64

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(1, lc)
	sched.Start()
	defer sched.Stop()

	const total = 1000
	for i := 0; i < total; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(3))
	}

	deadline := time.Now().Add(30 * time.Second)
	for completed.Load() < total && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != total {
		t.Fatalf("expected %d completed, got %d", total, completed.Load())
	}
}

// Stress test: All processes complete
func TestAllProcessesComplete(t *testing.T) {
	var completed atomic.Int64

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(4, lc)
	sched.Start()
	defer sched.Stop()

	const n = 100

	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(2))
	}

	// Wait for all to complete
	deadline := time.Now().Add(10 * time.Second)
	for completed.Load() < n && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != n {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}
}

// Stress test: Rapid sequential Execute calls with single worker
func TestSequentialExecuteSingleWorker(t *testing.T) {
	te := newTestExecutor(1)
	te.Start()
	defer te.Stop()

	for i := 0; i < 100; i++ {
		pid := relay.PID{UniqID: fmt.Sprintf("seq-%d", i)}
		result, err := te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput(3))
		if err != nil {
			t.Fatalf("iteration %d: execute error: %v", i, err)
		}
		if result == nil {
			t.Fatalf("iteration %d: nil result", i)
		}
	}
}

// Stress test: Many workers competing for few tasks
func TestHighContentionFewTasks(t *testing.T) {
	var completed atomic.Int32

	lc := &edgeTestLifecycle{
		onComplete: func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		},
	}
	sched := newEdgeTestScheduler(32, lc)
	sched.Start()
	defer sched.Stop()

	// Only 10 tasks for 32 workers
	for i := 0; i < 10; i++ {
		sched.Submit(context.Background(), testPID(), &CounterProcess{}, "", testInput(5))
	}

	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < 10 && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	if completed.Load() != 10 {
		t.Fatalf("expected 10 completed, got %d", completed.Load())
	}
}

// Stress test: Wakeup latency after idle
func BenchmarkWakeupLatency(b *testing.B) {
	te := newTestExecutor(4)
	te.Start()
	defer te.Stop()

	// Workers are idle, measure the overhead of idle detection
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Brief idle period
		time.Sleep(100 * time.Microsecond)
		// Single task
		pid := relay.PID{UniqID: fmt.Sprintf("wake-%d", i)}
		te.Execute(context.Background(), pid, &CounterProcess{}, "", testInput(1))
	}
}
