// SPDX-License-Identifier: MPL-2.0

package backoff

import (
	"math/rand/v2"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/supervisor"
)

// Calculator computes retry intervals using configurable backoff and jitter.
type Calculator struct {
	policy      supervisor.RetryPolicy
	mu          sync.Mutex
	attempt     int
	baseBackoff time.Duration
}

// NewCalculator creates a new Calculator with the given retry policy.
// MaxAttempts=0 means infinite retries.
func NewCalculator(policy supervisor.RetryPolicy) *Calculator {
	// Ensure BackoffFactor is not zero
	if policy.BackoffFactor <= 0 {
		policy.BackoffFactor = 1.0 // No backoff if factor is invalid
	}

	return &Calculator{
		policy:      policy,
		attempt:     0,
		baseBackoff: policy.InitialDelay,
	}
}

// NextInterval returns the duration to wait before the next retry attempt.
// Returns 0 if no more retries should be attempted.
func (b *Calculator) NextInterval() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.hasRemainingAttempts() {
		return 0
	}

	b.attempt++
	if b.attempt > 1 {
		// Apply exponential backoff only if BackoffFactor is valid
		b.baseBackoff = time.Duration(float64(b.baseBackoff) * b.policy.BackoffFactor)
		// Apply MaxDelay limit only if it's set
		if b.policy.MaxDelay > 0 && b.baseBackoff > b.policy.MaxDelay {
			b.baseBackoff = b.policy.MaxDelay
		}
	}

	return b.calculateIntervalWithJitter()
}

// Reset resets the attempt counter and backoff duration.
func (b *Calculator) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
	b.baseBackoff = b.policy.InitialDelay
}

// hasRemainingAttempts checks if retries are available.
// MaxAttempts=0 means infinite retries.
func (b *Calculator) hasRemainingAttempts() bool {
	return b.policy.MaxAttempts == 0 || b.attempt < b.policy.MaxAttempts
}

// calculateIntervalWithJitter applies jitter to the base backoff interval.
func (b *Calculator) calculateIntervalWithJitter() time.Duration {
	if b.baseBackoff == 0 {
		return 0
	}

	// If no jitter is configured, return base backoff
	if b.policy.Jitter <= 0 {
		return b.baseBackoff
	}

	// Calculate jitter as a random value between -jitter and +jitter
	jitterRange := float64(b.baseBackoff) * b.policy.Jitter
	jitter := (rand.Float64() * 2 * jitterRange) - jitterRange //nolint:gosec // ok for now

	interval := b.baseBackoff + time.Duration(jitter)
	if interval < 0 {
		interval = 0
	}

	return interval
}
