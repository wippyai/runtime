package supervisor

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/ponyruntime/pony/api/supervisor"
)

// Retrier handles the retry logic for an operation.
type Retrier struct {
	policy      supervisor.RetryPolicy
	mu          sync.Mutex
	timer       *time.Timer
	attempt     int
	baseBackoff time.Duration
}

// NewRetrier creates a new Retrier with the given retry policy.
func NewRetrier(policy supervisor.RetryPolicy) *Retrier {
	// Handle edge case where MaxAttempts is 0
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1 // Ensure at least one attempt
	}

	// Ensure BackoffFactor is not zero
	if policy.BackoffFactor <= 0 {
		policy.BackoffFactor = 1.0 // No backoff if factor is invalid
	}

	return &Retrier{
		policy:      policy,
		timer:       nil,
		attempt:     0,
		baseBackoff: policy.InitialDelay,
	}
}

// Start initiates a retry attempt.
func (r *Retrier) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attempt++
	if r.attempt > 1 {
		// Apply exponential backoff only if BackoffFactor is valid
		r.baseBackoff = time.Duration(float64(r.baseBackoff) * r.policy.BackoffFactor)
		// Apply MaxDelay limit only if it's set
		if r.policy.MaxDelay > 0 && r.baseBackoff > r.policy.MaxDelay {
			r.baseBackoff = r.policy.MaxDelay
		}
	}

	delay := r.calculateDelayWithJitter()
	if delay == 0 {
		// Skip creating timer for zero delays
		return
	}

	// Create or reset the timer
	if r.timer == nil {
		r.timer = time.NewTimer(delay)
	} else {
		r.resetTimer(delay)
	}
}

// NextAttempt waits until the next retry attempt should be made.
func (r *Retrier) NextAttempt(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.attempt >= r.policy.MaxAttempts {
		return false // Max attempts reached
	}

	// Handle zero delay case
	if r.baseBackoff == 0 {
		return true
	}

	// Wait for either timer completion or context cancellation
	if r.timer != nil {
		select {
		case <-r.timer.C:
			return true
		case <-ctx.Done():
			r.stopTimer()
			return false
		}
	}

	return true
}

// Stop stops the internal timer, releasing its resources.
func (r *Retrier) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopTimer()
}

// ShouldRetry returns true if the retry attempts are not exhausted.
func (r *Retrier) ShouldRetry() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempt < r.policy.MaxAttempts
}

// calculateDelayWithJitter calculates the delay with jitter if configured.
func (r *Retrier) calculateDelayWithJitter() time.Duration {
	if r.baseBackoff == 0 {
		return 0
	}

	// If no jitter is configured, return base backoff
	if r.policy.Jitter <= 0 {
		return r.baseBackoff
	}

	// Calculate jitter as a random value between -jitter and +jitter
	jitterRange := float64(r.baseBackoff) * r.policy.Jitter
	jitter := (rand.Float64() * 2 * jitterRange) - jitterRange

	delay := r.baseBackoff + time.Duration(jitter)
	if delay < 0 {
		delay = 0
	}

	return delay
}

// stopTimer is a helper to stop the timer and drain the channel if needed.
func (r *Retrier) stopTimer() {
	if r.timer != nil {
		if !r.timer.Stop() {
			select {
			case <-r.timer.C:
			default:
			}
		}
		r.timer = nil
	}
}

// resetTimer is a helper to reset the timer with the given duration.
func (r *Retrier) resetTimer(d time.Duration) {
	if !r.timer.Stop() {
		select {
		case <-r.timer.C:
		default:
		}
	}
	r.timer.Reset(d)
}
