// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constant for process group scopes.
const (
	// Scope identifies a process group scope in the registry.
	// Each scope is an independent PG instance with its own state,
	// event loop, and cluster mesh — following Erlang/OTP pg scope semantics.
	Scope registry.Kind = "pg.scope"
)

// Config defines configuration for a process group scope.
type Config struct {
	// ID is the registry entry ID, set automatically by DecodeEntryConfig.
	ID registry.ID `json:"id"`

	// Lifecycle configures supervisor lifecycle management.
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`

	// ProtocolTimeout is the timeout for sync/discover operations with remote nodes.
	// Operations exceeding this timeout are cancelled. Zero means no timeout.
	// Default: 5s.
	ProtocolTimeout time.Duration `json:"protocol_timeout"`

	// BroadcastTimeout is the timeout for sending broadcast messages to members.
	// Zero means no timeout (use circuit breaker only).
	// Default: 5s.
	BroadcastTimeout time.Duration `json:"broadcast_timeout"`

	// AntiEntropyInterval is the cadence of the periodic reconcile that
	// re-syncs local group state with cluster membership peers, so any
	// join/leave broadcast a peer missed eventually converges. One peer is
	// synced per tick (round-robin) to avoid fan-out storms. Zero disables
	// anti-entropy. Default: 30s.
	AntiEntropyInterval time.Duration `json:"anti_entropy_interval"`

	// CircuitBreakerResetTime is the duration after which a circuit breaker
	// will transition from open to half-open, allowing test requests.
	// Default: 10s.
	CircuitBreakerResetTime time.Duration `json:"circuit_breaker_reset_time"`

	// RetryBaseDelay is the initial delay between retries (exponential backoff).
	// Default: 100ms.
	RetryBaseDelay time.Duration `json:"retry_base_delay"`

	// RetryMaxDelay is the maximum delay between retries.
	// Default: 1s.
	RetryMaxDelay time.Duration `json:"retry_max_delay"`

	// ActionQueueSize is the depth at which the event loop queue is
	// considered "approaching capacity" and emits a warning. Must be <=
	// ActionQueueMaxSize. Default: 256.
	ActionQueueSize int `json:"action_queue_size"`

	// ActionQueueMaxSize is the hard capacity of the internal event loop
	// action channel. When the channel is full, new operations are dropped
	// non-blocking (counted as pg_queue_dropped_total{reason="full"}).
	// Default: 1024.
	ActionQueueMaxSize int `json:"action_queue_max_size"`

	// MonitorBuffer is the capacity of the per-monitor delivery channel.
	// Each monitor subscription gets a buffered channel of this size for
	// receiving membership events. If the buffer fills, events are dropped
	// for that subscriber (back-pressure). Default: 64.
	MonitorBuffer int `json:"monitor_buffer"`

	// MaxGroups limits the total number of distinct groups a scope can track.
	// Zero means unlimited (no cap). Attempts to join a new group when at
	// the limit return an error. Default: 0 (unlimited).
	MaxGroups int `json:"max_groups"`

	// MaxMembersPerGroup limits how many member slots a single group can hold.
	// Zero means unlimited (no cap). Because a PID may join the same group
	// multiple times, this counts total join slots, not unique PIDs.
	// Default: 0 (unlimited).
	MaxMembersPerGroup int `json:"max_members_per_group"`

	// CircuitBreakerFailures is the number of consecutive send failures before
	// opening the circuit breaker for a node. Default: 3.
	CircuitBreakerFailures int `json:"circuit_breaker_failures"`

	// MaxRetries is the maximum number of retry attempts for failed broadcasts.
	// Zero disables retries. Default: 3.
	MaxRetries int `json:"max_retries"`
}

// InitDefaults initializes the configuration with sensible defaults.
// Called by DecodeEntryConfig after unmarshaling.
func (c *Config) InitDefaults() {
	if c.ActionQueueSize == 0 {
		c.ActionQueueSize = 256
	}

	if c.ActionQueueMaxSize == 0 {
		c.ActionQueueMaxSize = 1024
	}

	if c.MonitorBuffer == 0 {
		c.MonitorBuffer = 64
	}

	if c.ProtocolTimeout == 0 {
		c.ProtocolTimeout = 5 * time.Second
	}

	if c.BroadcastTimeout == 0 {
		c.BroadcastTimeout = 5 * time.Second
	}

	if c.AntiEntropyInterval == 0 {
		c.AntiEntropyInterval = 30 * time.Second
	}

	if c.CircuitBreakerFailures == 0 {
		c.CircuitBreakerFailures = 3
	}

	if c.CircuitBreakerResetTime == 0 {
		c.CircuitBreakerResetTime = 10 * time.Second
	}

	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}

	if c.RetryBaseDelay == 0 {
		c.RetryBaseDelay = 100 * time.Millisecond
	}

	if c.RetryMaxDelay == 0 {
		c.RetryMaxDelay = time.Second
	}

	c.Lifecycle.InitDefaults()
}

// Validate checks if the configuration is valid.
// Called by DecodeEntryConfig after InitDefaults.
func (c *Config) Validate() error {
	if c.ActionQueueSize < 0 {
		return ErrInvalidActionQueueSize
	}

	if c.ActionQueueMaxSize < c.ActionQueueSize {
		return ErrInvalidActionQueueMaxSize
	}

	if c.MonitorBuffer < 0 {
		return ErrInvalidMonitorBuffer
	}

	if c.MaxGroups < 0 {
		return ErrInvalidMaxGroups
	}

	if c.MaxMembersPerGroup < 0 {
		return ErrInvalidMaxMembersPerGroup
	}

	if c.CircuitBreakerFailures < 0 {
		return ErrInvalidCircuitBreakerFailures
	}

	if c.MaxRetries < 0 {
		return ErrInvalidMaxRetries
	}

	if c.AntiEntropyInterval < 0 {
		return ErrInvalidAntiEntropyInterval
	}

	return nil
}
