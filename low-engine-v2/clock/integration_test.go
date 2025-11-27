package clock

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/low-engine-v2/scheduler"
)

// SleepProcess is a test process that sleeps for a configured duration.
type SleepProcess struct {
	duration time.Duration
	step     int
}

func (p *SleepProcess) Start(ctx context.Context, input payload.Payloads) error {
	p.step = 0
	return nil
}

func (p *SleepProcess) Step(results *scheduler.YieldResults) (scheduler.StepResult, error) {
	p.step++

	if p.step == 1 {
		// First step: yield sleep command
		return scheduler.StepResult{
			Status:     scheduler.StepContinue,
			YieldCount: 1,
			YieldsBuf:  [scheduler.MaxYields]scheduler.Command{SleepCmd{Duration: p.duration}},
		}, nil
	}

	// Second step: done
	return scheduler.StepResult{Status: scheduler.StepDone}, nil
}

func (p *SleepProcess) Send(pkg *relay.Package) error {
	return nil
}

func newTestScheduler(numWorkers int, opts ...scheduler.Option) *scheduler.Scheduler {
	registry := scheduler.NewRegistry()
	Register(registry)
	opts = append([]scheduler.Option{scheduler.WithWorkers(numWorkers)}, opts...)
	return scheduler.NewScheduler(registry, opts...)
}

func testPID() relay.PID {
	return relay.PID{UniqID: "test"}
}

// TestSleepHandler tests basic sleep functionality
func TestSleepHandler(t *testing.T) {
	sched := newTestScheduler(4)
	sched.Start()
	defer sched.Stop()

	start := time.Now()
	result, err := sched.Execute(context.Background(), testPID(), &SleepProcess{duration: 50 * time.Millisecond}, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("nil result")
	}

	if elapsed < 50*time.Millisecond {
		t.Fatalf("sleep too short: %v", elapsed)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("sleep too long: %v", elapsed)
	}
}

// TestParallelSleep tests parallel sleep commands
func TestParallelSleep(t *testing.T) {
	var completed atomic.Int32

	sched := newTestScheduler(runtime.GOMAXPROCS(0),
		scheduler.WithOnComplete(func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		}),
	)
	sched.Start()
	defer sched.Stop()

	const n = 100
	start := time.Now()

	// Submit n parallel sleeps
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &SleepProcess{duration: 10 * time.Millisecond}, nil)
	}

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for completed.Load() < int32(n) && time.Now().Before(deadline) {
		runtime.Gosched()
	}

	elapsed := time.Since(start)

	if completed.Load() != int32(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}

	// All 100 sleeps should complete in ~10-20ms (parallel), not 1000ms (serial)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("parallel sleep took too long: %v (expected ~10-50ms)", elapsed)
	}
	t.Logf("100 parallel 10ms sleeps completed in %v", elapsed)
}

// TestMassiveSleep tests 10,000 processes each sleeping for 10ms
func TestMassiveSleep(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping massive sleep test in short mode")
	}

	const n = 10000
	sleepDuration := 10 * time.Millisecond
	var completed atomic.Int64

	numWorkers := runtime.GOMAXPROCS(0)
	sched := newTestScheduler(numWorkers,
		scheduler.WithOnComplete(func(ctx context.Context, pid relay.PID, result *apiruntime.Result) {
			completed.Add(1)
		}),
	)
	sched.Start()
	defer sched.Stop()

	start := time.Now()

	// Submit n processes
	for i := 0; i < n; i++ {
		sched.Submit(context.Background(), testPID(), &SleepProcess{duration: sleepDuration}, nil)
	}

	// Wait for completion
	deadline := time.Now().Add(30 * time.Second)
	for completed.Load() < int64(n) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	elapsed := time.Since(start)

	if completed.Load() != int64(n) {
		t.Fatalf("expected %d completed, got %d", n, completed.Load())
	}

	t.Logf("Results:")
	t.Logf("  Workers: %d", numWorkers)
	t.Logf("  Processes: %d", n)
	t.Logf("  Sleep duration: %v", sleepDuration)
	t.Logf("  Total time: %v", elapsed)
	t.Logf("  Per-process overhead: %v", (elapsed-sleepDuration)/time.Duration(n))

	// Theoretical minimum is sleepDuration (all parallel)
	// Practical minimum depends on timer resolution and scheduler overhead
	// With 10k processes and 10ms sleep, should complete in ~100-500ms
	if elapsed > 5*time.Second {
		t.Fatalf("massive sleep took too long: %v", elapsed)
	}
}

// BenchmarkSleepThroughput benchmarks sleep handler throughput
func BenchmarkSleepThroughput(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	ctx := context.Background()
	pid := testPID()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Execute(ctx, pid, &SleepProcess{duration: time.Microsecond}, nil)
	}
}

// BenchmarkSleepParallel benchmarks parallel sleep
func BenchmarkSleepParallel(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		pid := testPID()
		for pb.Next() {
			sched.Execute(ctx, pid, &SleepProcess{duration: time.Microsecond}, nil)
		}
	})
}
