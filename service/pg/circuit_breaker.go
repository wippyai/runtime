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
	tel          *telemetry
	nodeID       string
	state        CircuitState
	failures     int
	maxFailures  int
	resetTimeout time.Duration
	mu           sync.RWMutex
}

// newCircuitBreaker creates a new circuit breaker with the given configuration.
func newCircuitBreaker(nodeID string, maxFailures int, resetTimeout time.Duration, logger *zap.Logger, tel *telemetry) *circuitBreaker {
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
		tel:          tel,
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
			cb.tel.recordCircuitBreakerState(cb.nodeID, "half-open")
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
		cb.tel.recordCircuitBreakerState(cb.nodeID, "closed")
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
		cb.tel.recordCircuitBreakerState(cb.nodeID, "open")
		cb.tel.recordCircuitBreakerTrip(cb.nodeID)
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
		cb.tel.recordCircuitBreakerState(cb.nodeID, "open")
		cb.tel.recordCircuitBreakerTrip(cb.nodeID)
	}
}

// State returns the current circuit state.
func (cb *circuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// defaultBreakerMapCap is the maximum number of per-node breakers the
// manager retains. Hit only as a defense-in-depth backstop: in normal
// operation the NodeLeft event triggers RemoveCircuitBreaker on every
// departure, so the table stays at cluster size. The cap protects
// against arbitrary nodeIDs accumulating if NodeLeft is missed under
// chaos partition.
const defaultBreakerMapCap = 1024

// circuitBreakerManager manages circuit breakers for multiple target nodes.
type circuitBreakerManager struct {
	breakers     map[string]*circuitBreaker
	logger       *zap.Logger
	tel          *telemetry
	maxFailures  int
	maxBreakers  int
	resetTimeout time.Duration
	mu           sync.RWMutex
}

// newCircuitBreakerManager creates a new circuit breaker manager.
func newCircuitBreakerManager(maxFailures int, resetTimeout time.Duration, logger *zap.Logger, tel *telemetry) *circuitBreakerManager {
	return &circuitBreakerManager{
		breakers:     make(map[string]*circuitBreaker),
		maxFailures:  maxFailures,
		maxBreakers:  defaultBreakerMapCap,
		resetTimeout: resetTimeout,
		logger:       logger,
		tel:          tel,
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

	// Defense-in-depth: if the table is at cap (NodeLeft cleanup missed
	// under chaos / split-brain), drop an arbitrary entry to keep the map
	// bounded. Drops are counted so dashboards can surface the symptom.
	if len(m.breakers) >= m.maxBreakers {
		var victim string
		for k := range m.breakers {
			victim = k
			break
		}
		delete(m.breakers, victim)
		m.tel.recordCircuitBreakerEvicted("cap")
	}

	cb = newCircuitBreaker(nodeID, m.maxFailures, m.resetTimeout, m.logger, m.tel)
	m.breakers[nodeID] = cb
	return cb
}

// RemoveCircuitBreaker removes the circuit breaker for a node.
func (m *circuitBreakerManager) RemoveCircuitBreaker(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.breakers, nodeID)
}
