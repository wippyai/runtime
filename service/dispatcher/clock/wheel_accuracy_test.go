package clock

import (
	"context"
	"math"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestWheelTimerAccuracy verifies that timers fire within acceptable tolerance.
// Due to OS scheduling and timing wheel tick granularity (1ms), we expect:
// - Most timers to fire within 2-3ms of expected time
// - Worst case within 10-15ms (under load or GC pressure)
func TestWheelTimerAccuracy(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	testCases := []struct {
		name      string
		duration  time.Duration
		maxJitter time.Duration
	}{
		{"5ms", 5 * time.Millisecond, 5 * time.Millisecond},
		{"10ms", 10 * time.Millisecond, 5 * time.Millisecond},
		{"50ms", 50 * time.Millisecond, 5 * time.Millisecond},
		{"100ms", 100 * time.Millisecond, 5 * time.Millisecond},
		{"500ms", 500 * time.Millisecond, 10 * time.Millisecond},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			start := time.Now()
			id := r.Start(tc.duration)
			fireTime, err := r.Wait(ctx, id)
			if err != nil {
				t.Fatalf("Wait error: %v", err)
			}

			actual := fireTime.Sub(start)
			jitter := actual - tc.duration
			if jitter < 0 {
				jitter = -jitter
			}

			t.Logf("expected=%v actual=%v jitter=%v", tc.duration, actual, jitter)

			if jitter > tc.maxJitter {
				t.Errorf("jitter %v exceeds max %v", jitter, tc.maxJitter)
			}
		})
	}
}

// TestWheelTimerAccuracyStatistical runs many timers and checks statistical distribution.
func TestWheelTimerAccuracyStatistical(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		timerCount = 100
		duration   = 20 * time.Millisecond
	)

	jitters := make([]time.Duration, timerCount)
	var wg sync.WaitGroup

	for i := 0; i < timerCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := context.Background()

			start := time.Now()
			id := r.Start(duration)
			fireTime, err := r.Wait(ctx, id)
			if err != nil {
				return
			}

			actual := fireTime.Sub(start)
			jitter := actual - duration
			if jitter < 0 {
				jitter = -jitter
			}
			jitters[idx] = jitter
		}(i)
	}

	wg.Wait()

	// Calculate statistics
	sort.Slice(jitters, func(i, j int) bool { return jitters[i] < jitters[j] })

	var sum time.Duration
	for _, j := range jitters {
		sum += j
	}
	mean := sum / time.Duration(timerCount)

	p50 := jitters[timerCount/2]
	p90 := jitters[timerCount*90/100]
	p99 := jitters[timerCount*99/100]
	max := jitters[timerCount-1]

	t.Logf("Jitter stats for %d timers at %v:", timerCount, duration)
	t.Logf("  mean=%v p50=%v p90=%v p99=%v max=%v", mean, p50, p90, p99, max)

	// Assertions - timing wheel with 1ms tick should achieve these
	if p50 > 3*time.Millisecond {
		t.Errorf("p50 jitter %v too high (expected <3ms)", p50)
	}
	if p90 > 5*time.Millisecond {
		t.Errorf("p90 jitter %v too high (expected <5ms)", p90)
	}
}

// TestWheelTimerOrderPreservation verifies timers fire in correct order.
func TestWheelTimerOrderPreservation(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	const timerCount = 20

	type result struct {
		expectedOrder int
		fireTime      time.Time
	}

	results := make(chan result, timerCount)
	ctx := context.Background()

	// Start timers with increasing delays
	for i := 0; i < timerCount; i++ {
		delay := time.Duration(10+i*5) * time.Millisecond
		go func(order int, d time.Duration) {
			id := r.Start(d)
			fireTime, err := r.Wait(ctx, id)
			if err == nil {
				results <- result{order, fireTime}
			}
		}(i, delay)
	}

	// Collect results
	collected := make([]result, 0, timerCount)
	for i := 0; i < timerCount; i++ {
		collected = append(collected, <-results)
	}

	// Sort by fire time
	sort.Slice(collected, func(i, j int) bool {
		return collected[i].fireTime.Before(collected[j].fireTime)
	})

	// Check order - allow some tolerance for timers with similar durations
	outOfOrder := 0
	for i := 0; i < len(collected)-1; i++ {
		if collected[i].expectedOrder > collected[i+1].expectedOrder+1 {
			outOfOrder++
		}
	}

	t.Logf("Order preservation: %d/%d timers fired in expected order",
		timerCount-outOfOrder, timerCount)

	if outOfOrder > timerCount/10 {
		t.Errorf("too many timers out of order: %d/%d", outOfOrder, timerCount)
	}
}

// TestWheelTimerCallbackAccuracy tests callback-style timers.
func TestWheelTimerCallbackAccuracy(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	const duration = 25 * time.Millisecond

	done := make(chan time.Duration, 1)
	start := time.Now()

	r.StartWithCallback(duration, func() {
		done <- time.Since(start)
	})

	select {
	case actual := <-done:
		jitter := actual - duration
		if jitter < 0 {
			jitter = -jitter
		}
		t.Logf("callback: expected=%v actual=%v jitter=%v", duration, actual, jitter)
		if jitter > 5*time.Millisecond {
			t.Errorf("callback jitter %v too high", jitter)
		}
	case <-time.After(duration + 50*time.Millisecond):
		t.Fatal("callback never fired")
	}
}

// TestWheelTimerVsStandardTimer compares timing wheel with standard Go timers.
func TestWheelTimerVsStandardTimer(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		timerCount = 50
		duration   = 30 * time.Millisecond
	)

	// Test wheel timer
	wheelJitters := make([]time.Duration, timerCount)
	for i := 0; i < timerCount; i++ {
		start := time.Now()
		id := r.Start(duration)
		_, _ = r.Wait(context.Background(), id)
		actual := time.Since(start)
		jitter := actual - duration
		if jitter < 0 {
			jitter = -jitter
		}
		wheelJitters[i] = jitter
	}

	// Test standard Go timer
	stdJitters := make([]time.Duration, timerCount)
	for i := 0; i < timerCount; i++ {
		start := time.Now()
		<-time.After(duration)
		actual := time.Since(start)
		jitter := actual - duration
		if jitter < 0 {
			jitter = -jitter
		}
		stdJitters[i] = jitter
	}

	// Calculate means
	var wheelSum, stdSum time.Duration
	for i := 0; i < timerCount; i++ {
		wheelSum += wheelJitters[i]
		stdSum += stdJitters[i]
	}
	wheelMean := wheelSum / time.Duration(timerCount)
	stdMean := stdSum / time.Duration(timerCount)

	// Calculate standard deviation
	var wheelVariance, stdVariance float64
	for i := 0; i < timerCount; i++ {
		wheelVariance += math.Pow(float64(wheelJitters[i]-wheelMean), 2)
		stdVariance += math.Pow(float64(stdJitters[i]-stdMean), 2)
	}
	wheelStdDev := time.Duration(math.Sqrt(wheelVariance / float64(timerCount)))
	stdStdDev := time.Duration(math.Sqrt(stdVariance / float64(timerCount)))

	t.Logf("Wheel timer:    mean=%v stddev=%v", wheelMean, wheelStdDev)
	t.Logf("Standard timer: mean=%v stddev=%v", stdMean, stdStdDev)

	// Wheel should be comparable to standard timer (within 2x jitter)
	if wheelMean > 2*stdMean+5*time.Millisecond {
		t.Errorf("wheel timer jitter significantly worse than standard: %v vs %v",
			wheelMean, stdMean)
	}
}

// TestWheelTimerUnderLoad tests accuracy under concurrent load.
func TestWheelTimerUnderLoad(t *testing.T) {
	r := NewWheelTimerRegistry()
	defer r.Close()

	const (
		concurrency = 100
		duration    = 50 * time.Millisecond
	)

	jitters := make([]time.Duration, concurrency)
	var wg sync.WaitGroup

	// Start all timers at once
	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := r.Start(duration)
			fireTime, _ := r.Wait(context.Background(), id)
			actual := fireTime.Sub(start)
			jitter := actual - duration
			if jitter < 0 {
				jitter = -jitter
			}
			jitters[idx] = jitter
		}(i)
	}

	wg.Wait()

	// Calculate stats
	sort.Slice(jitters, func(i, j int) bool { return jitters[i] < jitters[j] })
	p50 := jitters[concurrency/2]
	p99 := jitters[concurrency*99/100]
	max := jitters[concurrency-1]

	t.Logf("Under load (%d concurrent): p50=%v p99=%v max=%v", concurrency, p50, p99, max)

	// Under concurrent load, allow more jitter
	if p99 > 10*time.Millisecond {
		t.Errorf("p99 jitter under load too high: %v", p99)
	}
}
