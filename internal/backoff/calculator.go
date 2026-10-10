// SPDX-License-Identifier: MPL-2.0

package backoff

import (
	"math"
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
	if policy.BackoffFactor <= 0 || math.IsNaN(policy.BackoffFactor) {
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
		limit := time.Duration(math.MaxInt64)
		if b.policy.MaxDelay > 0 {
			limit = b.policy.MaxDelay
		}
		b.baseBackoff = boundedDuration(float64(b.baseBackoff)*b.policy.BackoffFactor, limit)
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
	if b.policy.Jitter <= 0 || math.IsNaN(b.policy.Jitter) || math.IsInf(b.policy.Jitter, 0) {
		return b.baseBackoff
	}

	// Calculate jitter as a random value between -jitter and +jitter
	factor := 1 + (rand.Float64()*2-1)*b.policy.Jitter //nolint:gosec // retry jitter is not security-sensitive
	return boundedDuration(float64(b.baseBackoff)*factor, time.Duration(math.MaxInt64))
}

// Clamp before conversion: converting an overflowing float to Duration can
// produce a negative value and accidentally turn a long backoff into no wait.
func boundedDuration(value float64, limit time.Duration) time.Duration {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value >= float64(limit) {
		return limit
	}
	return time.Duration(value)
}
