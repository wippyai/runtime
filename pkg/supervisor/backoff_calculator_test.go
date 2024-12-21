package supervisor

import (
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
)

func TestNewBackoffCalculator(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		MaxAttempts:   5,
	}

	calc := NewBackoffCalculator(policy)

	if calc.policy != policy {
		t.Errorf("Expected policy %v, got %v", policy, calc.policy)
	}
	if calc.attempt != 0 {
		t.Errorf("Expected initial attempt 0, got %d", calc.attempt)
	}
	if calc.baseBackoff != policy.InitialDelay {
		t.Errorf("Expected initial baseBackoff %v, got %v", policy.InitialDelay, calc.baseBackoff)
	}
}

func TestBackoffCalculator_NextInterval(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0, // No jitter for predictable testing
		MaxAttempts:   3,
	}

	calc := NewBackoffCalculator(policy)

	// Expected intervals for each attempt
	expectedIntervals := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		0, // No more retries
	}

	for i, expected := range expectedIntervals {
		interval := calc.NextInterval()
		if interval != expected {
			t.Errorf("Attempt %d: expected interval %v, got %v", i+1, expected, interval)
		}
	}
}

func TestBackoffCalculator_Reset(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0,
		MaxAttempts:   3,
	}

	calc := NewBackoffCalculator(policy)

	// Use up some attempts
	calc.NextInterval()
	calc.NextInterval()

	// Reset
	calc.Reset()

	if calc.attempt != 0 {
		t.Errorf("Expected attempt count to be 0 after reset, got %d", calc.attempt)
	}
	if calc.baseBackoff != policy.InitialDelay {
		t.Errorf("Expected baseBackoff to be reset to %v, got %v", policy.InitialDelay, calc.baseBackoff)
	}

	// Verify we can get the initial interval again
	interval := calc.NextInterval()
	if interval != policy.InitialDelay {
		t.Errorf("Expected initial interval after reset, got %v", interval)
	}
}

func TestBackoffCalculator_Jitter(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1000 * time.Millisecond,
		BackoffFactor: 1.0,
		Jitter:        0.2,
		MaxAttempts:   100,
	}

	calc := NewBackoffCalculator(policy)

	// Test multiple attempts to verify jitter range
	for i := 0; i < 50; i++ {
		interval := calc.NextInterval()

		// Calculate expected range
		minInterval := time.Duration(float64(policy.InitialDelay) * (1 - policy.Jitter))
		maxInterval := time.Duration(float64(policy.InitialDelay) * (1 + policy.Jitter))

		if interval < minInterval || interval > maxInterval {
			t.Errorf("Interval %v outside expected range [%v, %v]", interval, minInterval, maxInterval)
		}
	}
}

func TestBackoffCalculator_MaxDelay(t *testing.T) {
	policy := supervisor.RetryPolicy{
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      300 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        0,
		MaxAttempts:   5,
	}

	calc := NewBackoffCalculator(policy)

	// First interval should be initial delay
	interval := calc.NextInterval()
	if interval != 100*time.Millisecond {
		t.Errorf("Expected initial interval 100ms, got %v", interval)
	}

	// Second interval should be 200ms
	interval = calc.NextInterval()
	if interval != 200*time.Millisecond {
		t.Errorf("Expected second interval 200ms, got %v", interval)
	}

	// Third and subsequent intervals should be capped at 300ms
	interval = calc.NextInterval()
	if interval != 300*time.Millisecond {
		t.Errorf("Expected interval to be capped at 300ms, got %v", interval)
	}
}
