package clock

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestTimerWaveLoad simulates realistic wave-based load patterns.
// Creates timers in waves with varying intensity over time.
func TestTimerWaveLoad(t *testing.T) {
	r := NewTimerRegistry()
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

				duration := time.Duration(10+rand.Intn(100)) * time.Millisecond
				id := r.Start(duration)
				totalCreated.Add(1)

				// 30% of timers will be canceled
				if rand.Float32() < 0.3 {
					go func(id uint64) {
						time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
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
							r.Stop(id)
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
				interval := time.Duration(20+rand.Intn(80)) * time.Millisecond
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
					for j := 0; j < rand.Intn(5)+1; j++ {
						ctx2, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
						if _, err := r.Next(ctx2, id); err == nil {
							totalTicks.Add(1)
						}
						cancel()
					}
					r.Stop(id)
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
	r := NewTimerRegistry()
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
					duration := time.Duration(50+rand.Intn(200)) * time.Millisecond
					id := r.Start(duration)
					totalCreated.Add(1)

					ctx2, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
					defer cancel()
					if _, err := r.Wait(ctx2, id); err == nil {
						totalFired.Add(1)
					} else {
						r.Stop(id)
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
	r := NewTimerRegistry()
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

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			var localTimers []uint64

			// Each worker has different "personality"
			createBias := 0.3 + rng.Float64()*0.4 // 30-70% create bias
			cancelBias := 0.1 + rng.Float64()*0.2 // 10-30% cancel bias

			for {
				select {
				case <-ctx.Done():
					// Cleanup
					for _, id := range localTimers {
						r.Stop(id)
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
					r.Stop(id)
					cancels.Add(1)

				case roll < createBias+cancelBias+0.1 && len(localTimers) > 0:
					// Reset random timer
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					newDuration := time.Duration(10+rng.Intn(100)) * time.Millisecond
					r.Reset(id, newDuration)
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
					} else if err == context.DeadlineExceeded {
						timeouts.Add(1)
						r.Stop(id)
					} else {
						r.Stop(id)
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
	r := NewTimerRegistry()
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
			rng := rand.New(rand.NewSource(time.Now().UnixNano()))
			for j := 0; j < resetsEach*timers/resetWorkers; j++ {
				idx := rng.Intn(timers)
				duration := time.Duration(rng.Intn(100)+1) * time.Millisecond
				r.Reset(ids[idx], duration)
			}
		}()
	}

	wg.Wait()

	// Cleanup
	for _, id := range ids {
		r.Stop(id)
	}

	t.Log("Concurrent reset race test passed")
}

// TestWheelTimerMixedChaos tests timing wheel under chaotic load.
func TestWheelTimerMixedChaos(t *testing.T) {
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

			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			var localTimers []uint64

			for {
				select {
				case <-ctx.Done():
					for _, id := range localTimers {
						r.Stop(id)
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
					r.Stop(id)
					cancels.Add(1)

				case op < 70 && len(localTimers) > 0: // Reset
					idx := rng.Intn(len(localTimers))
					id := localTimers[idx]
					newDuration := time.Duration(5+rng.Intn(50)) * time.Millisecond
					r.Reset(id, newDuration)
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
						r.Stop(id)
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
	r := NewTimerRegistry()
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
	r := NewTimerRegistry()
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
				r.Stop(id)
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
	// Timer registry
	tr := NewTimerRegistry()
	for i := 0; i < 1000; i++ {
		id := tr.Start(time.Hour)
		tr.Stop(id)
	}
	if count := tr.Count(); count != 0 {
		t.Errorf("timer registry leaked %d timers", count)
	}
	tr.Close()

	// Ticker registry
	tkr := NewTickerRegistry()
	for i := 0; i < 1000; i++ {
		id := tkr.Start(time.Hour)
		tkr.Stop(id)
	}
	tkr.Close()

	// Wheel timer registry
	wr := NewWheelTimerRegistry()
	for i := 0; i < 1000; i++ {
		id := wr.Start(time.Hour)
		wr.Stop(id)
	}
	if count := wr.Count(); count != 0 {
		t.Errorf("wheel timer registry leaked %d timers", count)
	}
	wr.Close()
}

// TestContextCancellationUnderLoad tests context cancellation with many waiters.
func TestContextCancellationUnderLoad(t *testing.T) {
	r := NewTimerRegistry()
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
					time.Sleep(time.Microsecond * time.Duration(rand.Intn(100)))
					cancel()
				}()

				_, err := r.Wait(ctx, id)
				if err == context.Canceled {
					cancelled.Add(1)
				}
				r.Stop(id)
			}
		}()
	}

	wg.Wait()

	t.Logf("Context cancellations: %d/%d", cancelled.Load(), workers*perWorker)
}
