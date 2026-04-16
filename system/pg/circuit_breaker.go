// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed means the circuit is closed and requests flow normally.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit is open and requests fail immediately.
	CircuitOpen
	// CircuitHalfOpen means the circuit is testing if the downstream has recovered.
	CircuitHalfOpen
)

// String returns a human-readable representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// circuitBreaker implements the circuit breaker pattern for per-node failure isolation.
// It tracks consecutive failures and opens the circuit after a threshold,
// preventing cascading failures to slow/unavailable nodes.
type circuitBreaker struct {
	lastFailure  time.Time
	openTime     time.Time
	logger       *zap.Logger
	nodeID       string
	state        CircuitState
	failures     int
	maxFailures  int
	resetTimeout time.Duration
	mu           sync.RWMutex
}

// newCircuitBreaker creates a new circuit breaker with the given configuration.
func newCircuitBreaker(nodeID string, maxFailures int, resetTimeout time.Duration, logger *zap.Logger) *circuitBreaker {
	if maxFailures <= 0 {
		maxFailures = 3
	}
	if resetTimeout <= 0 {
		resetTimeout = 10 * time.Second
	}
	return &circuitBreaker{
		state:        CircuitClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		logger:       logger,
		nodeID:       nodeID,
	}
}

// Allow checks if a request should be allowed through.
// Returns true if the circuit is closed or half-open.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitClosed {
		return true
	}

	if cb.state == CircuitOpen {
		// Check if we should transition to half-open
		if time.Since(cb.openTime) >= cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.failures = 0
			cb.logger.Debug("circuit breaker transitioned to half-open",
				zap.String("node", cb.nodeID),
			)
			return true
		}
		return false
	}

	// Half-open: allow the test request
	return true
}

// RecordSuccess records a successful request, resetting failure count.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitHalfOpen {
		// Successfully tested, close the circuit
		cb.state = CircuitClosed
		cb.failures = 0
		cb.logger.Info("circuit breaker closed after successful test",
			zap.String("node", cb.nodeID),
		)
	} else if cb.state == CircuitClosed && cb.failures > 0 {
		// Reset failure count on success in closed state
		cb.failures = 0
	}
}

// RecordFailure records a failed request, potentially opening the circuit.
func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == CircuitHalfOpen {
		// Test request failed, go back to open
		cb.state = CircuitOpen
		cb.openTime = time.Now()
		cb.logger.Warn("circuit breaker opened (test request failed)",
			zap.String("node", cb.nodeID),
			zap.Int("failures", cb.failures),
		)
		return
	}

	if cb.state == CircuitClosed && cb.failures >= cb.maxFailures {
		// Too many failures, open the circuit
		cb.state = CircuitOpen
		cb.openTime = time.Now()
		cb.logger.Warn("circuit breaker opened",
			zap.String("node", cb.nodeID),
			zap.Int("failures", cb.failures),
			zap.Duration("reset_after", cb.resetTimeout),
		)
	}
}

// State returns the current circuit state.
func (cb *circuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// circuitBreakerManager manages circuit breakers for multiple target nodes.
type circuitBreakerManager struct {
	breakers     map[string]*circuitBreaker
	logger       *zap.Logger
	maxFailures  int
	resetTimeout time.Duration
	mu           sync.RWMutex
}

// newCircuitBreakerManager creates a new circuit breaker manager.
func newCircuitBreakerManager(maxFailures int, resetTimeout time.Duration, logger *zap.Logger) *circuitBreakerManager {
	return &circuitBreakerManager{
		breakers:     make(map[string]*circuitBreaker),
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
		logger:       logger,
	}
}

// GetCircuitBreaker returns the circuit breaker for a node, creating it if necessary.
func (m *circuitBreakerManager) GetCircuitBreaker(nodeID string) *circuitBreaker {
	m.mu.RLock()
	cb, exists := m.breakers[nodeID]
	m.mu.RUnlock()

	if exists {
		return cb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if cb, exists = m.breakers[nodeID]; exists {
		return cb
	}

	cb = newCircuitBreaker(nodeID, m.maxFailures, m.resetTimeout, m.logger)
	m.breakers[nodeID] = cb
	return cb
}

// RemoveCircuitBreaker removes the circuit breaker for a node.
func (m *circuitBreakerManager) RemoveCircuitBreaker(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.breakers, nodeID)
}
