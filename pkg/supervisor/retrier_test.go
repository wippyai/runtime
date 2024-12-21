package supervisor

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
)

// TestNewRetrier tests the creation of a new Retrier.
func TestNewRetrier(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)

	if retrier.policy != policy {
		t.Errorf("Expected policy %v, got %v", policy, retrier.policy)
	}

	if retrier.attempt != 0 {
		t.Errorf("Expected initial attempt 0, got %d", retrier.attempt)
	}

	if retrier.baseBackoff != policy.InitialDelay {
		t.Errorf("Expected initial baseBackoff %v, got %v", policy.InitialDelay, retrier.baseBackoff)
	}
}

// TestRetrier_Start tests the Start method of the Retrier.
func TestRetrier_Start(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)

	retrier.Start()

	if retrier.attempt != 1 {
		t.Errorf("Expected attempt 1, got %d", retrier.attempt)
	}

	if retrier.baseBackoff != policy.InitialDelay {
		t.Errorf("Expected baseBackoff %v, got %v", policy.InitialDelay, retrier.baseBackoff)
	}

	if policy.InitialDelay > 0 && retrier.timer == nil {
		t.Error("Expected timer to be initialized, got nil")
	}
}

// TestRetrier_NextAttempt_WithinMaxAttempts tests the NextAttempt method within the maximum number of attempts.
func TestRetrier_NextAttempt_WithinMaxAttempts(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   3,
	}

	retrier := NewRetrier(policy)

	for i := 0; i < policy.MaxAttempts-1; i++ {
		retrier.Start()
		if !retrier.NextAttempt(context.Background()) {
			t.Errorf("Expected NextAttempt to return true, got false on attempt %d", i+1)
		}
	}
}

// TestRetrier_NextAttempt_ExceedMaxAttempts tests the NextAttempt method exceeding the maximum number of attempts.
func TestRetrier_NextAttempt_ExceedMaxAttempts(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   3,
	}

	retrier := NewRetrier(policy)

	for i := 0; i < policy.MaxAttempts; i++ {
		retrier.Start()
		retrier.NextAttempt(context.Background()) // Consume the attempt
	}

	if retrier.NextAttempt(context.Background()) {
		t.Error("Expected NextAttempt to return false after exceeding max attempts, got true")
	}
}

// TestRetrier_NextAttempt_ContextCancellation tests the NextAttempt method with context cancellation.
func TestRetrier_NextAttempt_ContextCancellation(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)
	retrier.Start()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel the context immediately

	if retrier.NextAttempt(ctx) {
		t.Error("Expected NextAttempt to return false with cancelled context, got true")
	}
}

// TestRetrier_Stop tests the Stop method of the Retrier.
func TestRetrier_Stop(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)
	retrier.Start()
	retrier.Stop()

	if retrier.timer != nil {
		t.Error("Expected timer to be nil after Stop, but it wasn't")
	}
}

// TestRetrier_ShouldRetry tests the ShouldRetry method of the Retrier.
func TestRetrier_ShouldRetry(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   3,
	}

	retrier := NewRetrier(policy)

	if !retrier.ShouldRetry() {
		t.Error("Expected ShouldRetry to return true initially, got false")
	}

	for i := 0; i < policy.MaxAttempts-1; i++ {
		retrier.Start()
		retrier.NextAttempt(context.Background())
	}

	if !retrier.ShouldRetry() {
		t.Error("Expected ShouldRetry to return true before reaching max attempts, got false")
	}

	retrier.Start()
	retrier.NextAttempt(context.Background()) // Consume the last attempt

	if retrier.ShouldRetry() {
		t.Error("Expected ShouldRetry to return false after reaching max attempts, got true")
	}
}

// TestRetrier_ExponentialBackoff tests the exponential backoff calculation.
func TestRetrier_ExponentialBackoff(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      500 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0, // No jitter for this test
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)

	expectedBackoffs := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond, // Max delay reached
		500 * time.Millisecond, // Should stay at max delay
	}

	for i, expected := range expectedBackoffs {
		retrier.Start()
		if retrier.baseBackoff != expected {
			t.Errorf("Expected baseBackoff on attempt %d to be %v, got %v", i+1, expected, retrier.baseBackoff)
		}
		retrier.NextAttempt(context.Background()) // Move to the next attempt
	}
}

// TestRetrier_Jitter tests the jitter calculation.
func TestRetrier_Jitter(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 1.0, // No backoff for this test
		Jitter:        0.2,
		MaxAttempts:   20, // Many attempts to observe jitter distribution
	}

	retrier := NewRetrier(policy)

	for i := 0; i < policy.MaxAttempts; i++ {
		retrier.Start()

		// Skip jitter check for zero delay
		if policy.InitialDelay == 0 {
			continue
		}

		// Get the actual delay from the timer
		startTime := time.Now()
		<-retrier.timer.C
		actualDelay := time.Since(startTime)

		// Jitter should be within +/- 20% of InitialDelay (100ms * 0.2 = 20ms)
		// Use a tolerance of 1.1 to account for very small timing differences
		minDelay := policy.InitialDelay - time.Duration(float64(policy.InitialDelay)*policy.Jitter*1.1)
		maxDelay := policy.InitialDelay + time.Duration(float64(policy.InitialDelay)*policy.Jitter*1.1)

		// Check if the actual delay is within the expected range
		if actualDelay < minDelay || actualDelay > maxDelay {
			t.Errorf("Expected delay on attempt %d to be between %v and %v, got %v", i+1, minDelay, maxDelay, actualDelay)
		}
	}
}

// TestRetrier_stopTimer tests the stopTimer helper method of the Retrier.
func TestRetrier_stopTimer(t *testing.T) {
	retrier := NewRetrier(supervisor.RetryPolicy{InitialDelay: 100 * time.Millisecond})

	retrier.Start()
	time.Sleep(50 * time.Millisecond) // Allow some time for the timer to potentially fire

	retrier.stopTimer()

	if retrier.timer != nil {
		t.Error("Expected timer to be nil after stopTimer, but it wasn't")
	}
}

// TestRetrier_resetTimer tests the resetTimer helper method of the Retrier.
func TestRetrier_resetTimer(t *testing.T) {
	retrier := NewRetrier(supervisor.RetryPolicy{InitialDelay: 100 * time.Millisecond})

	retrier.Start()
	time.Sleep(50 * time.Millisecond) // Allow some time for the timer to potentially fire

	newDuration := 200 * time.Millisecond
	retrier.resetTimer(newDuration)

	if retrier.timer == nil {
		t.Fatal("Expected timer to be reset, but it was nil")
	}

	// Check if the new duration is approximately correct
	startTime := time.Now()
	<-retrier.timer.C
	elapsed := time.Since(startTime)

	// Allow for some variance due to timing in tests
	if math.Abs(elapsed.Seconds()-newDuration.Seconds()) > 0.05 {
		t.Errorf("Expected timer to be reset to approximately %v, but it fired after %v", newDuration, elapsed)
	}
}

// TestRetrier_ConcurrentAccess tests the Retrier for concurrent access safety.
func TestRetrier_ConcurrentAccess(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 1.5,
		Jitter:        0.1,
		MaxAttempts:   10,
	}
	retrier := NewRetrier(policy)

	// Number of goroutines to simulate concurrent access.
	numGoroutines := 10

	// WaitGroup to wait for all goroutines to complete.
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Channel to signal when all goroutines have started.
	startSignal := make(chan struct{})

	// Function executed by each goroutine.
	goroutineFunc := func() {
		defer wg.Done()
		<-startSignal // Wait for the start signal.

		// Perform a series of operations on the Retrier.
		for i := 0; i < 20; i++ {
			retrier.Start()
			if retrier.NextAttempt(context.Background()) {
				// Simulate some work that takes a little time.
				time.Sleep(time.Duration(rand.Intn(5)) * time.Millisecond)
			}
			if i%2 == 0 {
				retrier.Stop()
			}
		}
	}

	// Start the goroutines.
	for i := 0; i < numGoroutines; i++ {
		go goroutineFunc()
	}

	// Signal all goroutines to start at once.
	close(startSignal)

	// Wait for all goroutines to complete.
	wg.Wait()
}

// TestRetrier_EdgeCases tests edge cases for the Retrier such as zero values, very large values, etc.
func TestRetrier_EdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		policy        supervisor.RetryPolicy
		expectRetries bool
	}{
		{
			name: "ZeroInitialDelay",
			policy: supervisor.RetryPolicy{
				InitialDelay:  0,
				MaxDelay:      100 * time.Millisecond,
				BackoffFactor: 2.0,
				Jitter:        0.1,
				MaxAttempts:   5,
			},
			expectRetries: true, // Should still retry with no delay
		},
		{
			name: "ZeroMaxDelay",
			policy: supervisor.RetryPolicy{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      0,
				BackoffFactor: 2.0,
				Jitter:        0.1,
				MaxAttempts:   5,
			},
			expectRetries: true, // Should use initial delay
		},
		{
			name: "ZeroBackoffFactor",
			policy: supervisor.RetryPolicy{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1000 * time.Millisecond,
				BackoffFactor: 0,
				Jitter:        0.1,
				MaxAttempts:   5,
			},
			expectRetries: true, // Should keep initial delay
		},
		{
			name: "ZeroJitter",
			policy: supervisor.RetryPolicy{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1000 * time.Millisecond,
				BackoffFactor: 2.0,
				Jitter:        0,
				MaxAttempts:   5,
			},
			expectRetries: true, // Should work without jitter
		},
		{
			name: "ZeroMaxAttempts",
			policy: supervisor.RetryPolicy{
				InitialDelay:  100 * time.Millisecond,
				MaxDelay:      1000 * time.Millisecond,
				BackoffFactor: 2.0,
				Jitter:        0.1,
				MaxAttempts:   0,
			},
			expectRetries: false, // Should not retry at all
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			retrier := NewRetrier(tc.policy)
			ctx := context.Background()

			// For policies that should allow retries
			if tc.expectRetries {
				// Test that we can make MaxAttempts-1 successful retries
				for i := 0; i < tc.policy.MaxAttempts-1; i++ {
					retrier.Start()
					if !retrier.NextAttempt(ctx) {
						t.Errorf("Expected NextAttempt to return true on attempt %d, got false", i+1)
					}
				}

				// The last attempt should return false
				retrier.Start()
				if retrier.NextAttempt(ctx) {
					t.Error("Expected final NextAttempt to return false")
				}
			} else {
				// For ZeroMaxAttempts, should never allow retries
				retrier.Start()
				if retrier.NextAttempt(ctx) {
					t.Error("Expected NextAttempt to return false with zero MaxAttempts")
				}
			}
		})
	}
}

// TestRetrier_StopTimer_DefaultCase tests the default case in stopTimer.
func TestRetrier_StopTimer_DefaultCase(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)
	retrier.Start()

	// Wait for timer to fire
	time.Sleep(15 * time.Millisecond)

	// Now stop the timer - this should hit the default case
	retrier.Stop()

	if retrier.timer != nil {
		t.Error("Timer not properly cleaned up")
	}
}

// TestRetrier_ResetTimer_DefaultCase tests the default case in resetTimer.
func TestRetrier_ResetTimer_DefaultCase(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	retrier := NewRetrier(policy)
	retrier.Start()

	// Wait for timer to fire
	time.Sleep(15 * time.Millisecond)

	// Reset timer - this should hit the default case
	startTime := time.Now()
	retrier.resetTimer(20 * time.Millisecond)
	<-retrier.timer.C
	elapsed := time.Since(startTime)

	if math.Abs(elapsed.Seconds()-0.02) > 0.01 {
		t.Errorf("Timer not reset correctly. Expected ~20ms, got %v", elapsed)
	}
}
