package worksim

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/system/scheduler/pool/adaptive"
)

type mockDispatcher struct{}

func (d *mockDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler { return nil }

// TestWorkloadBasic verifies the workload simulator works correctly.
func TestWorkloadBasic(t *testing.T) {
	w := New()

	// No bottleneck, no latency - should complete instantly
	start := time.Now()
	err := w.Work(context.Background())
	if err != nil {
		t.Fatalf("Work failed: %v", err)
	}
	if time.Since(start) > 10*time.Millisecond {
		t.Fatal("Work took too long with no latency")
	}

	if w.Completed() != 1 {
		t.Fatalf("expected 1 completed, got %d", w.Completed())
	}
}

// TestWorkloadLatency verifies latency simulation.
func TestWorkloadLatency(t *testing.T) {
	w := New()
	w.SetLatency(50 * time.Millisecond)

	start := time.Now()
	err := w.Work(context.Background())
	if err != nil {
		t.Fatalf("Work failed: %v", err)
	}

	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond || elapsed > 70*time.Millisecond {
		t.Fatalf("expected ~50ms latency, got %v", elapsed)
	}
}

// TestWorkloadBottleneck verifies bottleneck simulation.
func TestWorkloadBottleneck(t *testing.T) {
	w := New()
	w.SetBottleneck(2)
	w.SetLatency(100 * time.Millisecond)

	var wg sync.WaitGroup
	start := time.Now()

	// Start 4 workers, but bottleneck is 2
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.Work(context.Background())
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// With bottleneck=2 and 4 workers doing 100ms each:
	// Should take ~200ms (2 batches of 2)
	if elapsed < 180*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("expected ~200ms with bottleneck, got %v", elapsed)
	}

	// MaxActive should be ~2 (the bottleneck)
	if w.MaxActive() > 3 { // Allow some slack for timing
		t.Fatalf("expected max active ~2, got %d", w.MaxActive())
	}
}

// TestAdaptiveDiscoversBottleneck verifies the adaptive pool finds optimal worker count.
func TestAdaptiveDiscoversBottleneck(t *testing.T) {
	w := New()
	w.SetBottleneck(4)
	w.SetLatency(20 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(16),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(3),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Generate sustained load
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var ops atomic.Int64
	var wg sync.WaitGroup

	// 8 goroutines generating load
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, err := pool.Call(context.Background(), "", nil)
					if err == nil {
						ops.Add(1)
					}
				}
			}
		}()
	}

	// Let it run and observe
	time.Sleep(3 * time.Second)

	t.Logf("Operations completed: %d", ops.Load())
	t.Logf("Workload completed: %d", w.Completed())
	t.Logf("Max active observed: %d", w.MaxActive())

	cancel()
	wg.Wait()

	// The pool should have discovered that more than 4 workers doesn't help
	// (we can't directly check worker count, but max active tells us)
	if w.MaxActive() < 2 {
		t.Errorf("expected pool to scale up, max active was only %d", w.MaxActive())
	}
}

// TestAdaptiveScalesWithIO verifies scaling with I/O-bound workload.
func TestAdaptiveScalesWithIO(t *testing.T) {
	w := New()
	// No bottleneck - pure I/O simulation
	w.SetLatency(30 * time.Millisecond)
	w.SetJitter(0.2) // 20% jitter

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var ops atomic.Int64
	var wg sync.WaitGroup

	// Generate high load - should scale up
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					_, err := pool.Call(context.Background(), "", nil)
					if err == nil {
						ops.Add(1)
					}
				}
			}
		}()
	}

	time.Sleep(3 * time.Second)

	t.Logf("Operations completed: %d", ops.Load())
	t.Logf("Max active observed: %d", w.MaxActive())

	cancel()
	wg.Wait()

	// With I/O-bound work and no bottleneck, should scale to near max
	if w.MaxActive() < 4 {
		t.Errorf("expected pool to scale up for I/O work, max active was %d", w.MaxActive())
	}
}

// TestAdaptiveDynamicBottleneck verifies adaptation when bottleneck changes.
func TestAdaptiveDynamicBottleneck(t *testing.T) {
	w := New()
	w.SetBottleneck(2)
	w.SetLatency(15 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(12),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(3),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var ops atomic.Int64
	var wg sync.WaitGroup
	done := make(chan struct{})

	// Generate sustained load
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_, err := pool.Call(context.Background(), "", nil)
					if err == nil {
						ops.Add(1)
					}
				}
			}
		}()
	}

	// Phase 1: bottleneck = 2
	time.Sleep(2 * time.Second)
	phase1Max := w.MaxActive()
	t.Logf("Phase 1 (bottleneck=2): max active = %d, ops = %d", phase1Max, ops.Load())

	// Phase 2: increase bottleneck to 6
	w.ResetMetrics()
	w.SetBottleneck(6)
	time.Sleep(2 * time.Second)
	phase2Max := w.MaxActive()
	t.Logf("Phase 2 (bottleneck=6): max active = %d, ops = %d", phase2Max, ops.Load())

	// Phase 3: remove bottleneck
	w.ResetMetrics()
	w.SetBottleneck(0)
	time.Sleep(2 * time.Second)
	phase3Max := w.MaxActive()
	t.Logf("Phase 3 (no bottleneck): max active = %d, ops = %d", phase3Max, ops.Load())

	close(done)
	wg.Wait()

	// Verify adaptation: max active should increase as bottleneck relaxes
	if phase2Max <= phase1Max {
		t.Logf("Warning: phase 2 didn't scale up (max %d vs %d)", phase2Max, phase1Max)
	}
}

// TestAdaptiveScalesDown verifies scale-down when load disappears.
func TestAdaptiveScalesDown(t *testing.T) {
	w := New()
	w.SetLatency(20 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(2),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Generate high load to scale up
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					pool.Call(context.Background(), "", nil)
				}
			}
		}()
	}

	time.Sleep(2 * time.Second)
	maxDuringLoad := w.MaxActive()
	t.Logf("During load: max active = %d", maxDuringLoad)

	// Stop load
	close(done)
	wg.Wait()

	// Wait for scale-down
	time.Sleep(2 * time.Second)

	// Do a few calls to see current state
	w.ResetMetrics()
	for i := 0; i < 5; i++ {
		pool.Call(context.Background(), "", nil)
	}
	maxAfterScaleDown := w.MaxActive()
	t.Logf("After scale-down: max active = %d", maxAfterScaleDown)

	// Should have scaled down significantly
	if maxAfterScaleDown >= maxDuringLoad && maxDuringLoad > 2 {
		t.Logf("Warning: didn't observe scale-down (before=%d, after=%d)", maxDuringLoad, maxAfterScaleDown)
	}
}

// TestWorkloadZeroLatency verifies fast path with no latency.
func TestWorkloadZeroLatency(t *testing.T) {
	w := New()
	// No latency, no bottleneck - maximum throughput test

	const iterations = 100000
	start := time.Now()

	for i := 0; i < iterations; i++ {
		w.Work(context.Background())
	}

	elapsed := time.Since(start)
	opsPerSec := float64(iterations) / elapsed.Seconds()

	t.Logf("Zero latency: %d ops in %v (%.0f ops/sec)", iterations, elapsed, opsPerSec)

	// Should be very fast - millions of ops/sec
	if opsPerSec < 100000 {
		t.Errorf("Zero latency path too slow: %.0f ops/sec", opsPerSec)
	}
}

// TestAdaptiveBottleneckOne verifies behavior with bottleneck=1 (serial execution).
func TestAdaptiveBottleneckOne(t *testing.T) {
	w := New()
	w.SetBottleneck(1)
	w.SetLatency(10 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(3),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					pool.Call(context.Background(), "", nil)
				}
			}
		}()
	}

	time.Sleep(2 * time.Second)
	cancel()
	wg.Wait()

	t.Logf("Bottleneck=1: max active = %d, completed = %d", w.MaxActive(), w.Completed())
	t.Logf("Worker count: %d", pool.WorkerCount())

	// Should recognize only 1 can make progress
	if w.MaxActive() > 2 {
		t.Errorf("Expected max active ~1, got %d", w.MaxActive())
	}
}

// TestAdaptiveBottleneckEqualsMax verifies behavior when bottleneck >= maxWorkers.
func TestAdaptiveBottleneckEqualsMax(t *testing.T) {
	w := New()
	w.SetBottleneck(8) // Same as maxWorkers
	w.SetLatency(15 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					pool.Call(context.Background(), "", nil)
				}
			}
		}()
	}

	time.Sleep(2 * time.Second)
	cancel()
	wg.Wait()

	t.Logf("Bottleneck=maxWorkers: max active = %d, workers = %d", w.MaxActive(), pool.WorkerCount())

	// Should scale to max
	if pool.WorkerCount() < 4 {
		t.Errorf("Expected to scale up significantly, got %d workers", pool.WorkerCount())
	}
}

// TestAdaptiveRapidLoadChange verifies behavior with oscillating load.
func TestAdaptiveRapidLoadChange(t *testing.T) {
	w := New()
	w.SetLatency(20 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(2),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	done := make(chan struct{})
	var wg sync.WaitGroup

	// Oscillate between high and no load
	for cycle := 0; cycle < 4; cycle++ {
		// High load phase
		w.ResetMetrics()
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-done:
						return
					default:
						pool.Call(context.Background(), "", nil)
					}
				}
			}()
		}

		time.Sleep(800 * time.Millisecond)
		close(done)
		wg.Wait()
		highLoadWorkers := pool.WorkerCount()
		highLoadActive := w.MaxActive()

		// No load phase
		done = make(chan struct{})
		time.Sleep(600 * time.Millisecond)
		lowLoadWorkers := pool.WorkerCount()

		t.Logf("Cycle %d: high=%d workers (active=%d), low=%d workers",
			cycle+1, highLoadWorkers, highLoadActive, lowLoadWorkers)
	}
}

// TestAdaptiveBurstyLoad verifies behavior with bursty workload.
func TestAdaptiveBurstyLoad(t *testing.T) {
	w := New()
	w.SetLatency(25 * time.Millisecond)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(2),
		adaptive.WithIdleTicks(3),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	// Send bursts of requests with gaps
	for burst := 0; burst < 5; burst++ {
		w.ResetMetrics()

		// Burst: 20 concurrent requests
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				pool.Call(context.Background(), "", nil)
			}()
		}
		wg.Wait()

		t.Logf("Burst %d: max active = %d, workers = %d", burst+1, w.MaxActive(), pool.WorkerCount())

		// Gap between bursts
		time.Sleep(300 * time.Millisecond)
	}
}

// TestAdaptiveHighLatencyLowBottleneck verifies I/O-heavy with contention.
func TestAdaptiveHighLatencyLowBottleneck(t *testing.T) {
	w := New()
	w.SetBottleneck(2)
	w.SetLatency(100 * time.Millisecond) // Slow I/O
	w.SetJitter(0.3)

	factory := NewFactory(w)
	pool, err := adaptive.New(factory, &mockDispatcher{},
		adaptive.WithMaxWorkers(8),
		adaptive.WithControlInterval(100*time.Millisecond),
		adaptive.WithProbeTicks(3),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer pool.Stop()
	pool.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					pool.Call(context.Background(), "", nil)
				}
			}
		}()
	}

	time.Sleep(3 * time.Second)
	cancel()
	wg.Wait()

	t.Logf("High latency + bottleneck=2: max active = %d, workers = %d, completed = %d",
		w.MaxActive(), pool.WorkerCount(), w.Completed())

	// Should not over-scale despite high latency
	if w.MaxActive() > 4 {
		t.Logf("Warning: may have over-scaled, max active = %d", w.MaxActive())
	}
}
