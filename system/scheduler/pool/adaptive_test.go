package pool

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/relay"
)

// testAdaptiveOptions returns options tuned for faster test execution
func testAdaptiveOptions(maxWorkers int) []AdaptiveOption {
	return []AdaptiveOption{
		WithMaxWorkers(maxWorkers),
		WithControlInterval(100 * time.Millisecond),
		WithProbeCooldown(300 * time.Millisecond),
		WithProbeFailedCooldown(500 * time.Millisecond),
		WithScaleDownCooldown(200 * time.Millisecond),
		WithIdleScaleDownTicks(2),
		WithQueuePressureOverrideTicks(3),
	}
}

func TestAdaptiveBasic(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(4)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	result, err := pool.Call(testContext(), "test", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result.Error != nil {
		t.Fatalf("result error: %v", result.Error)
	}
}

func TestAdaptiveConcurrent(t *testing.T) {
	factory, count := newCountingFactory()
	pool, err := NewAdaptive(factory, &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Initial worker count should be minWorkers (1)
	if count.Load() != 1 {
		t.Fatalf("expected 1 initial worker, got %d", count.Load())
	}

	var wg sync.WaitGroup
	const n = 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := pool.Call(testContext(), "test", nil)
			if err != nil {
				t.Errorf("Call: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestAdaptiveContextCancel(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(50*time.Millisecond), &mockDispatcher{}, testAdaptiveOptions(2)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx := testContext()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()

	result, err := pool.Call(ctx, "test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestAdaptiveStopDrainsQueue(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(10*time.Millisecond), &mockDispatcher{}, testAdaptiveOptions(2)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	pool.Start()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = pool.Call(testContext(), "test", nil)
		}()
	}

	time.Sleep(5 * time.Millisecond)
	pool.Stop()
	wg.Wait()
}

func TestAdaptiveScaleUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}

	// Use latency to ensure queue builds up
	factory := newMockFactory(5 * time.Millisecond)
	pool, err := NewAdaptive(factory, &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	initial := pool.workerCount.Load()
	if initial != 1 {
		t.Fatalf("expected 1 initial worker, got %d", initial)
	}

	// Generate sustained high load with slow operations
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, _ = pool.Call(testContext(), "test", nil)
				}
			}
		}()
	}

	// Let the controller run for a few cycles
	time.Sleep(3 * time.Second)
	cancel()
	wg.Wait()

	final := pool.workerCount.Load()
	t.Logf("Workers: initial=%d, final=%d", initial, final)

	// Should have scaled up from initial
	if final <= initial {
		t.Errorf("expected scale up, initial=%d, final=%d", initial, final)
	}
}

func TestAdaptiveScaleDown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}

	// Use latency to ensure scaling up
	pool, err := NewAdaptive(newMockFactory(5*time.Millisecond), &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Generate load to scale up
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, _ = pool.Call(testContext(), "test", nil)
				}
			}
		}()
	}

	time.Sleep(3 * time.Second)
	peakWorkers := pool.workerCount.Load()
	t.Logf("Peak workers: %d", peakWorkers)

	// Stop load
	cancel()
	wg.Wait()

	// Wait for scale down
	time.Sleep(2 * time.Second)
	finalWorkers := pool.workerCount.Load()
	t.Logf("Final workers: %d", finalWorkers)

	// Should have scaled down
	if finalWorkers >= peakWorkers && peakWorkers > 1 {
		t.Errorf("expected scale down, peak=%d, final=%d", peakWorkers, finalWorkers)
	}
}

func TestAdaptiveMinWorkers(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Check initial worker count
	if pool.workerCount.Load() != 1 {
		t.Errorf("expected minWorkers=1, got %d", pool.workerCount.Load())
	}

	// Wait and verify it doesn't go below min
	time.Sleep(1 * time.Second)
	if pool.workerCount.Load() < 1 {
		t.Errorf("workers went below minimum: %d", pool.workerCount.Load())
	}
}

func TestAdaptiveMaxWorkers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}

	const maxWorkers = 4
	pool, err := NewAdaptive(newMockFactory(10*time.Millisecond), &mockDispatcher{}, testAdaptiveOptions(maxWorkers)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Generate heavy load
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, _ = pool.Call(testContext(), "test", nil)
				}
			}
		}()
	}

	time.Sleep(3 * time.Second)
	cancel()
	wg.Wait()

	if pool.workerCount.Load() > int32(maxWorkers) {
		t.Errorf("exceeded maxWorkers: got %d, max %d", pool.workerCount.Load(), maxWorkers)
	}
}

func TestAdaptivePoolClosed(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(2)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	pool.Start()
	pool.Stop()

	_, err = pool.Call(testContext(), "test", nil)
	if err != ErrPoolClosed {
		t.Errorf("expected ErrPoolClosed, got %v", err)
	}
}

func TestAdaptiveControllerState(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Initial state should be stable
	if pool.state != stateStable {
		t.Errorf("expected stateStable, got %d", pool.state)
	}
}

func TestAdaptiveConcurrentCancellationStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewAdaptive(newSlowYieldingFactory(), &yieldingDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed, cancelled, errors atomic.Int64

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			ctx := context.Background()
			var cancel context.CancelFunc
			if id%4 == 0 {
				ctx, cancel = context.WithTimeout(ctx, time.Duration(id%15)*time.Millisecond)
				defer cancel()
			}

			result, err := pool.Call(ctx, "test", nil)
			if err != nil {
				if ctx.Err() != nil {
					cancelled.Add(1)
				} else {
					errors.Add(1)
				}
				return
			}
			if result.Error != nil {
				errors.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	wg.Wait()
	pool.Stop()

	t.Logf("Completed: %d, Cancelled: %d, Errors: %d",
		completed.Load(), cancelled.Load(), errors.Load())
}

func TestAdaptiveStopDuringExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pool, err := NewAdaptive(newSlowYieldingFactory(), &yieldingDispatcher{}, testAdaptiveOptions(4)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}

	pool.Start()

	var wg sync.WaitGroup
	var completed atomic.Int64

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := pool.Call(context.Background(), "test", nil)
			if err == nil && result.Error == nil {
				completed.Add(1)
			}
		}()
	}

	time.Sleep(5 * time.Millisecond)
	pool.Stop()
	wg.Wait()

	t.Logf("Completed after stop: %d/50", completed.Load())
}

func TestAdaptiveRapidStopStart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	for cycle := 0; cycle < 10; cycle++ {
		pool, err := NewAdaptive(newSlowYieldingFactory(), &yieldingDispatcher{}, testAdaptiveOptions(4)...)
		if err != nil {
			t.Fatalf("NewAdaptive: %v", err)
		}

		pool.Start()

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
				defer cancel()
				_, _ = pool.Call(ctx, "test", nil)
			}()
		}

		time.Sleep(2 * time.Millisecond)
		pool.Stop()
		wg.Wait()
	}
}

func TestAdaptiveEMACalculation(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(4)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Generate some load to initialize EMA
	for i := 0; i < 100; i++ {
		_, _ = pool.Call(testContext(), "test", nil)
	}

	// Wait for control loop to update EMA
	time.Sleep(300 * time.Millisecond)

	// EMA should be non-zero after processing requests
	if pool.EMA() == 0 {
		t.Error("EMA should be non-zero after processing requests")
	}
}

func TestAdaptiveQueuePressure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping queue pressure test in short mode")
	}

	pool, err := NewAdaptive(newMockFactory(5*time.Millisecond), &mockDispatcher{}, testAdaptiveOptions(8)...)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Generate sustained load to build queue pressure
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, _ = pool.Call(testContext(), "test", nil)
				}
			}
		}()
	}

	// Wait for queue pressure to trigger scaling
	time.Sleep(3 * time.Second)
	cancel()
	wg.Wait()

	t.Logf("Final workers: %d, highQueueTicks: %d", pool.workerCount.Load(), pool.HighQueueTicks())
}

func TestAdaptiveFunctionalOptions(t *testing.T) {
	pool, err := NewAdaptive(newMockFactory(0), &mockDispatcher{},
		WithMaxWorkers(16),
		WithControlInterval(2*time.Second),
		WithProbeCooldown(10*time.Second),
		WithImprovementThreshold(0.05),
		WithQueuePressureRatio(0.30),
	)
	if err != nil {
		t.Fatalf("NewAdaptive: %v", err)
	}
	defer pool.Stop()

	if pool.maxWorkers != 16 {
		t.Errorf("expected maxWorkers=16, got %d", pool.maxWorkers)
	}
	if pool.controlInterval != 2*time.Second {
		t.Errorf("expected controlInterval=2s, got %v", pool.controlInterval)
	}
	if pool.probeCooldown != 10*time.Second {
		t.Errorf("expected probeCooldown=10s, got %v", pool.probeCooldown)
	}
	if pool.improvementThreshold != 0.05 {
		t.Errorf("expected improvementThreshold=0.05, got %f", pool.improvementThreshold)
	}
	if pool.queuePressureRatio != 0.30 {
		t.Errorf("expected queuePressureRatio=0.30, got %f", pool.queuePressureRatio)
	}
}

// BenchmarkAdaptiveCall benchmarks adaptive pool throughput
func BenchmarkAdaptiveCall(b *testing.B) {
	pool, _ := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(8)...)
	defer pool.Stop()
	pool.Start()

	// Warm up
	for i := 0; i < 1000; i++ {
		_, _ = pool.Call(testContext(), "test", nil)
	}
	time.Sleep(1 * time.Second) // Let it scale

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = pool.Call(testContext(), "test", nil)
		}
	})
}

// BenchmarkAdaptiveVsStatic compares adaptive to static
func BenchmarkAdaptiveVsStatic(b *testing.B) {
	b.Run("Adaptive", func(b *testing.B) {
		pool, _ := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(8)...)
		defer pool.Stop()
		pool.Start()

		for i := 0; i < 1000; i++ {
			_, _ = pool.Call(testContext(), "test", nil)
		}
		time.Sleep(1 * time.Second)

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = pool.Call(testContext(), "test", nil)
			}
		})
	})

	b.Run("Static", func(b *testing.B) {
		pool, _ := NewStatic(newMockFactory(0), &mockDispatcher{}, Config{Workers: 8})
		defer pool.Stop()
		pool.Start()

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = pool.Call(testContext(), "test", nil)
			}
		})
	})
}

func BenchmarkAdaptiveSendLookup(b *testing.B) {
	pool, _ := NewAdaptive(newMockFactory(0), &mockDispatcher{}, testAdaptiveOptions(4)...)
	defer pool.Stop()
	pool.Start()

	executor := NewExecutor(&mockDispatcher{})
	executor.active.Store(true)
	executor.queue.Reset()
	executor.gen.Store(executor.queue.Generation())
	pool.active.Store("bench-1", executor)
	pkg := &relay.Package{Target: relay.PID{UniqID: "bench-1"}}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Send(pkg)
	}
}
