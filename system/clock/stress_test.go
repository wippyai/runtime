package clock

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTimerWaveLoad simulates realistic wave-based load patterns.
// Creates timers in waves with varying intensity over time.
func TestTimerWaveLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		testDuration = 10 * time.Second
		baseRate     = 100 // base timers per wave
		maxRate      = 500 // peak timers per wave
		waveInterval = 500 * time.Millisecond
	)

	var (
		totalCreated  atomic.Int64
		totalFired    atomic.Int64
		totalCanceled atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var wg sync.WaitGroup
	startTime := time.Now()

	// Wave generator - creates timers following sine wave pattern
	wg.Add(1)
	go func() {
		defer wg.Done()
		wave := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			elapsed := time.Since(startTime).Seconds()
			// Sine wave with period of 2 seconds
			intensity := 0.5 + 0.5*math.Sin(elapsed*math.Pi)
			timerCount := int(float64(baseRate) + float64(maxRate-baseRate)*intensity)

			for i := 0; i < timerCount; i++ {
				if ctx.Err() != nil {
					return
				}

				duration := time.Duration(10+rand.Intn(100)) * time.Millisecond //nolint:gosec // test simulation
				id := r.Start(duration)
				totalCreated.Add(1)

				// 30% of timers will be canceled
				if rand.Float32() < 0.3 { //nolint:gosec // test simulation
					go func(id uint64) {
						time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond) //nolint:gosec // test simulation
						if _, err := r.Stop(id); err == nil {
							totalCanceled.Add(1)
						}
					}(id)
				} else {
					// Wait for the timer
					go func(id uint64) {
						ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
						defer cancel()
						if _, err := r.Wait(ctx2, id); err == nil {
							totalFired.Add(1)
						} else {
							_, _ = r.Stop(id)
						}
					}(id)
				}
			}

			wave++
			time.Sleep(waveInterval)
		}
	}()

	wg.Wait()

	// Wait for all pending operations
	time.Sleep(200 * time.Millisecond)

	t.Logf("Wave load: created=%d fired=%d canceled=%d (remaining=%d)",
		totalCreated.Load(), totalFired.Load(), totalCanceled.Load(), r.Count())
}

// TestTickerWaveLoad simulates wave-based load patterns for tickers.
func TestTickerWaveLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewTickerRegistry()
	defer r.Close()

	const (
		testDuration = 10 * time.Second
		baseCount    = 20
		maxCount     = 100
		waveInterval = 1 * time.Second
	)

	var (
		totalCreated atomic.Int64
		totalTicks   atomic.Int64
		totalStopped atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	activeTickers := make(chan uint64, 1000)
	var wg sync.WaitGroup

	// Ticker creator - follows sine wave
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			elapsed := time.Since(time.Now()).Seconds()
			intensity := 0.5 + 0.5*math.Sin(elapsed*math.Pi*0.5)
			count := int(float64(baseCount) + float64(maxCount-baseCount)*intensity)

			for i := 0; i < count; i++ {
				if ctx.Err() != nil {
					return
				}
				interval := time.Duration(20+rand.Intn(80)) * time.Millisecond //nolint:gosec // test simulation
				id := r.Start(interval)
				totalCreated.Add(1)

				select {
				case activeTickers <- id:
				default:
				}
			}
			time.Sleep(waveInterval)
		}
	}()

	// Ticker consumers - read from tickers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-activeTickers:
					// Read a few ticks then stop
					for j := 0; j < rand.Intn(5)+1; j++ { //nolint:gosec // test simulation
						ctx2, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
						if _, err := r.Next(ctx2, id); err == nil {
							totalTicks.Add(1)
						}
						cancel()
					}
					_ = r.Stop(id)
					totalStopped.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Ticker wave load: created=%d ticks=%d stopped=%d",
		totalCreated.Load(), totalTicks.Load(), totalStopped.Load())
}

// TestTimerBurstSpikes simulates sudden traffic spikes followed by calm periods.
func TestTimerBurstSpikes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		testDuration = 15 * time.Second
		spikeSize    = 1000
		calmPeriod   = 2 * time.Second
	)

	var (
		totalCreated atomic.Int64
		totalFired   atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var wg sync.WaitGroup

	// Spike generator
	wg.Add(1)
	go func() {
		defer wg.Done()
		spikeNum := 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Create spike of timers all at once
			var spikeWg sync.WaitGroup
			for i := 0; i < spikeSize; i++ {
				if ctx.Err() != nil {
					return
				}
				spikeWg.Add(1)
				go func() {
					defer spikeWg.Done()
					duration := time.Duration(50+rand.Intn(200)) * time.Millisecond //nolint:gosec // test simulation
					id := r.Start(duration)
					totalCreated.Add(1)

					ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
					defer cancel()
					if _, err := r.Wait(ctx2, id); err == nil {
						totalFired.Add(1)
					} else {
						_, _ = r.Stop(id)
					}
				}()
			}
			spikeWg.Wait()

			spikeNum++
			t.Logf("Spike %d: %d timers, %d fired", spikeNum, spikeSize, totalFired.Load())

			// Calm period
			time.Sleep(calmPeriod)
		}
	}()

	wg.Wait()

	t.Logf("Burst spikes: created=%d fired=%d", totalCreated.Load(), totalFired.Load())
}

// TestMixedOperationsChaos performs random mixed operations with realistic patterns.
func TestMixedOperationsChaos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		workers      = 100
		testDuration = 10 * time.Second
	)

	var (
		creates  atomic.Int64
		waits    atomic.Int64
		cancels  atomic.Int64
		resets   atomic.Int64
		timeouts atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var wg sync.WaitGroup

	// Each worker simulates a "user" with varying activity patterns
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID))) //nolint:gosec // test simulation
			var localTimers []uint64

			// Each worker has different "personality"
			createBias := 0.3 + rng.Float64()*0.4 // 30-70% create bias
			cancelBias := 0.1 + rng.Float64()*0.2 // 10-30% cancel bias

			for {
				select {
				case <-ctx.Done():
					// Cleanup
					for _, id := range localTimers {
						_, _ = r.Stop(id)
					}
					return
				default:
				}

				// Add some jitter to simulate human behavior
				time.Sleep(time.Duration(rng.Intn(10)) * time.Millisecond)

				roll := rng.Float64()
				switch {
				case roll < createBias:
					// Create timer with varying durations
					duration := time.Duration(10+rng.Intn(190)) * time.Millisecond
					id := r.Start(duration)
					localTimers = append(localTimers, id)
					creates.Add(1)

				case roll < createBias+cancelBias && len(localTimers) > 0:
					// Cancel random timer
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					localTimers = append(localTimers[:idx], localTimers[idx+1:]...)
					_, _ = r.Stop(id)
					cancels.Add(1)

				case roll < createBias+cancelBias+0.1 && len(localTimers) > 0:
					// Reset random timer
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					newDuration := time.Duration(10+rng.Intn(100)) * time.Millisecond
					_, _ = r.Reset(id, newDuration)
					resets.Add(1)

				case len(localTimers) > 0:
					// Wait for random timer
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					localTimers = append(localTimers[:idx], localTimers[idx+1:]...)

					waitCtx, waitCancel := context.WithTimeout(ctx, 300*time.Millisecond)
					_, err := r.Wait(waitCtx, id)
					waitCancel()

					if err == nil {
						waits.Add(1)
					} else if errors.Is(err, context.DeadlineExceeded) {
						timeouts.Add(1)
						_, _ = r.Stop(id)
					} else {
						_, _ = r.Stop(id)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Mixed chaos: creates=%d waits=%d cancels=%d resets=%d timeouts=%d",
		creates.Load(), waits.Load(), cancels.Load(), resets.Load(), timeouts.Load())

	if remaining := r.Count(); remaining > 0 {
		t.Errorf("leaked %d timers", remaining)
	}
}

// TestConcurrentResetRace tests concurrent resets on the same timer to find races.
func TestConcurrentResetRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		timers       = 100
		resetsEach   = 100
		resetWorkers = 10
	)

	ids := make([]uint64, timers)
	for i := 0; i < timers; i++ {
		ids[i] = r.Start(time.Hour)
	}

	var wg sync.WaitGroup
	for i := 0; i < resetWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // test code
			for j := 0; j < resetsEach*timers/resetWorkers; j++ {
				idx := rng.Intn(timers)
				duration := time.Duration(rng.Intn(100)+1) * time.Millisecond
				_, _ = r.Reset(ids[idx], duration)
			}
		}()
	}

	wg.Wait()

	// Cleanup
	for _, id := range ids {
		_, _ = r.Stop(id)
	}

	t.Log("Concurrent reset race test passed")
}

// TestWheelTimerMixedChaos tests timing wheel under chaotic load.
func TestWheelTimerMixedChaos(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		workers      = 50
		testDuration = 10 * time.Second
	)

	var (
		creates atomic.Int64
		waits   atomic.Int64
		cancels atomic.Int64
		resets  atomic.Int64
		fired   atomic.Int64
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID))) //nolint:gosec // test simulation
			var localTimers []uint64

			for {
				select {
				case <-ctx.Done():
					for _, id := range localTimers {
						_, _ = r.Stop(id)
					}
					return
				default:
				}

				time.Sleep(time.Duration(rng.Intn(5)) * time.Millisecond)

				op := rng.Intn(100)
				switch {
				case op < 40: // Create
					duration := time.Duration(5+rng.Intn(95)) * time.Millisecond
					id := r.Start(duration)
					localTimers = append(localTimers, id)
					creates.Add(1)

				case op < 55 && len(localTimers) > 0: // Cancel
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					localTimers = append(localTimers[:idx], localTimers[idx+1:]...)
					_, _ = r.Stop(id)
					cancels.Add(1)

				case op < 70 && len(localTimers) > 0: // Reset
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					newDuration := time.Duration(5+rng.Intn(50)) * time.Millisecond
					_, _ = r.Reset(id, newDuration)
					resets.Add(1)

				case len(localTimers) > 0: // Wait
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					localTimers = append(localTimers[:idx], localTimers[idx+1:]...)

					waitCtx, waitCancel := context.WithTimeout(ctx, 200*time.Millisecond)
					_, err := r.Wait(waitCtx, id)
					waitCancel()

					if err == nil {
						waits.Add(1)
						fired.Add(1)
					} else {
						_, _ = r.Stop(id)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Wheel timer chaos: creates=%d waits=%d cancels=%d resets=%d fired=%d",
		creates.Load(), waits.Load(), cancels.Load(), resets.Load(), fired.Load())

	if remaining := r.Count(); remaining > 0 {
		t.Errorf("leaked %d timers", remaining)
	}
}

// TestRapidCreateWait rapidly creates and waits for timers.
func TestRapidCreateWait(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		workers   = 50
		perWorker = 1000
	)

	var wg sync.WaitGroup
	var errors atomic.Int64

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < perWorker; j++ {
				id := r.Start(time.Millisecond)
				if _, err := r.Wait(ctx, id); err != nil {
					errors.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Rapid create/wait: %d total, %d errors", workers*perWorker, errors.Load())
	if errors.Load() > 0 {
		t.Errorf("%d errors occurred", errors.Load())
	}
}

// TestRapidCreateCancel rapidly creates and cancels timers.
func TestRapidCreateCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		workers   = 100
		perWorker = 5000
	)

	var created, cancelled atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id := r.Start(time.Hour)
				created.Add(1)
				_, _ = r.Stop(id)
				cancelled.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Rapid create/cancel: %d created, %d cancelled", created.Load(), cancelled.Load())

	if created.Load() != cancelled.Load() {
		t.Errorf("mismatch: created=%d cancelled=%d", created.Load(), cancelled.Load())
	}

	if r.Count() != 0 {
		t.Errorf("leaked timers: %d", r.Count())
	}
}

// TestLeakDetection verifies no resources are leaked.
func TestLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	// Timer registry (wheel-based)
	tr := NewWheelTimerRegistry()
	for i := 0; i < 1000; i++ {
		id := tr.Start(time.Hour)
		_, _ = tr.Stop(id)
	}
	if count := tr.Count(); count != 0 {
		t.Errorf("timer registry leaked %d timers", count)
	}
	tr.Close()

	// Ticker registry
	tkr := NewTickerRegistry()
	for i := 0; i < 1000; i++ {
		id := tkr.Start(time.Hour)
		_ = tkr.Stop(id)
	}
	tkr.Close()
}

// TestContextCancellationUnderLoad tests context cancellation with many waiters.
func TestContextCancellationUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		workers   = 200
		perWorker = 100
	)

	var cancelled atomic.Int64
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				ctx, cancel := context.WithCancel(context.Background())
				id := r.Start(time.Hour)

				go func() {
					time.Sleep(time.Microsecond * time.Duration(rand.Intn(100))) //nolint:gosec // test simulation
					cancel()
				}()

				_, err := r.Wait(ctx, id)
				if errors.Is(err, context.Canceled) {
					cancelled.Add(1)
				}
				_, _ = r.Stop(id)
			}
		}()
	}

	wg.Wait()

	t.Logf("Context cancellations: %d/%d", cancelled.Load(), workers*perWorker)
}

// TestWheelTimerMemoryLeak tests for memory leaks by creating and destroying many timers.
func TestWheelTimerMemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	const iterations = 10
	const timersPerIteration = 10000

	for iter := 0; iter < iterations; iter++ {
		r := NewWheelTimerRegistry()

		// Create timers with mixed durations
		ids := make([]uint64, timersPerIteration)
		for i := 0; i < timersPerIteration; i++ {
			duration := time.Duration(1+rand.Intn(100)) * time.Millisecond //nolint:gosec // test simulation
			ids[i] = r.Start(duration)
		}

		// Wait for some, stop others
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		for i, id := range ids {
			if i%2 == 0 {
				_, _ = r.Wait(ctx, id)
			} else {
				_, _ = r.Stop(id)
			}
		}
		cancel()

		// Close registry
		r.Close()

		// Verify no remaining timers
		if count := r.Count(); count != 0 {
			t.Errorf("iteration %d: leaked %d timers", iter, count)
		}
	}

	runtime.GC()
	runtime.GC() // Second GC to clean up finalizers
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc) //nolint:gosec // test code
	if heapGrowth > 10*1024*1024 {                          // 10MB threshold
		t.Errorf("potential memory leak: heap grew by %d bytes after %d iterations",
			heapGrowth, iterations)
	}
	t.Logf("Memory: start=%dKB end=%dKB growth=%dKB",
		m1.HeapAlloc/1024, m2.HeapAlloc/1024, heapGrowth/1024)
}

// TestWheelTimerLongRunning simulates a long-running service with steady timer churn.
func TestWheelTimerLongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		duration    = 30 * time.Second
		targetRate  = 1000 // timers per second
		checkPeriod = 5 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var created, fired, stopped atomic.Int64
	var wg sync.WaitGroup

	// Producer: creates timers at target rate
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second / time.Duration(targetRate))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dur := time.Duration(10+rand.Intn(90)) * time.Millisecond //nolint:gosec // test simulation
				id := r.Start(dur)
				created.Add(1)

				// 70% wait, 30% stop immediately
				if rand.Intn(100) < 70 { //nolint:gosec // test simulation
					go func(id uint64) {
						waitCtx, waitCancel := context.WithTimeout(ctx, 200*time.Millisecond)
						if _, err := r.Wait(waitCtx, id); err == nil {
							fired.Add(1)
						} else {
							_, _ = r.Stop(id)
							stopped.Add(1)
						}
						waitCancel()
					}(id)
				} else {
					_, _ = r.Stop(id)
					stopped.Add(1)
				}
			}
		}
	}()

	// Monitor: periodically check for leaks
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(checkPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count := r.Count()
				c, f, s := created.Load(), fired.Load(), stopped.Load()
				outstanding := c - f - s
				t.Logf("Progress: created=%d fired=%d stopped=%d count=%d outstanding=%d",
					c, f, s, count, outstanding)
				if count > int(targetRate)*2 {
					t.Errorf("too many outstanding timers: %d", count)
				}
			}
		}
	}()

	wg.Wait()

	// Final check
	time.Sleep(500 * time.Millisecond) // Let remaining timers fire
	finalCount := r.Count()
	t.Logf("Final: created=%d fired=%d stopped=%d remaining=%d",
		created.Load(), fired.Load(), stopped.Load(), finalCount)
}

// TestWheelTimerBurstMemory tests memory behavior under burst load.
func TestWheelTimerBurstMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		bursts         = 5
		timersPerBurst = 50000
		burstDelay     = 2 * time.Second
	)

	runtime.GC()
	var baselineMem runtime.MemStats
	runtime.ReadMemStats(&baselineMem)

	for burst := 0; burst < bursts; burst++ {
		// Create burst of timers
		ids := make([]uint64, timersPerBurst)
		for i := 0; i < timersPerBurst; i++ {
			ids[i] = r.Start(time.Duration(10+rand.Intn(90)) * time.Millisecond) //nolint:gosec // test simulation
		}

		// Wait for all to fire
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		for _, id := range ids {
			_, _ = r.Wait(ctx, id)
		}
		cancel()

		// Check memory after burst
		runtime.GC()
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		t.Logf("Burst %d: heap=%dMB count=%d", burst+1, mem.HeapAlloc/1024/1024, r.Count())

		time.Sleep(burstDelay)
	}

	// Final memory check
	runtime.GC()
	runtime.GC()
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	growth := int64(finalMem.HeapAlloc) - int64(baselineMem.HeapAlloc) //nolint:gosec // test code
	t.Logf("Memory growth after %d bursts: %dKB (baseline=%dKB final=%dKB)",
		bursts, growth/1024, baselineMem.HeapAlloc/1024, finalMem.HeapAlloc/1024)

	if growth > 20*1024*1024 { // 20MB threshold
		t.Errorf("excessive memory growth: %d bytes", growth)
	}
}

// TestWheelTimerGoroutineLeak tests for goroutine leaks.
func TestWheelTimerGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	baseGoroutines := runtime.NumGoroutine()

	const iterations = 5
	for i := 0; i < iterations; i++ {
		r := NewWheelTimerRegistry()

		// Create and destroy timers
		for j := 0; j < 1000; j++ {
			id := r.Start(time.Millisecond)
			if j%2 == 0 {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				_, _ = r.Wait(ctx, id)
				cancel()
			} else {
				_, _ = r.Stop(id)
			}
		}

		r.Close()
	}

	// Let goroutines settle
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	leaked := finalGoroutines - baseGoroutines

	t.Logf("Goroutines: base=%d final=%d leaked=%d", baseGoroutines, finalGoroutines, leaked)

	if leaked > 5 { // Allow small variance
		t.Errorf("goroutine leak detected: %d goroutines leaked", leaked)
	}
}
