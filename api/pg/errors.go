// SPDX-License-Identifier: MPL-2.0

package pg

import apierror "github.com/wippyai/runtime/api/error"

var (
	// ErrInvalidActionQueueSize is returned when action_queue_size is negative.
	ErrInvalidActionQueueSize = apierror.New(apierror.Invalid, "pg: action queue size must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidActionQueueMaxSize is returned when action_queue_max_size is less than action_queue_size.
	ErrInvalidActionQueueMaxSize = apierror.New(apierror.Invalid, "pg: action queue max size must be >= action queue size").WithRetryable(apierror.False)

	// ErrInvalidMonitorBuffer is returned when monitor_buffer is negative.
	ErrInvalidMonitorBuffer = apierror.New(apierror.Invalid, "pg: monitor buffer must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMaxGroups is returned when max_groups is negative.
	ErrInvalidMaxGroups = apierror.New(apierror.Invalid, "pg: max groups must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMaxMembersPerGroup is returned when max_members_per_group is negative.
	ErrInvalidMaxMembersPerGroup = apierror.New(apierror.Invalid, "pg: max members per group must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidCircuitBreakerFailures is returned when circuit_breaker_failures is negative.
	ErrInvalidCircuitBreakerFailures = apierror.New(apierror.Invalid, "pg: circuit breaker failures must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMaxRetries is returned when max_retries is negative.
	ErrInvalidMaxRetries = apierror.New(apierror.Invalid, "pg: max retries must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidAntiEntropyInterval is returned when anti_entropy_interval is negative.
	ErrInvalidAntiEntropyInterval = apierror.New(apierror.Invalid, "pg: anti entropy interval must be non-negative").WithRetryable(apierror.False)

	// ErrMaxGroupsReached is returned when joining would exceed the max_groups limit.
	ErrMaxGroupsReached = apierror.New(apierror.RateLimited, "pg: maximum number of groups reached").WithRetryable(apierror.True)

	// ErrMaxMembersReached is returned when joining would exceed the max_members_per_group limit.
	ErrMaxMembersReached = apierror.New(apierror.RateLimited, "pg: maximum members per group reached").WithRetryable(apierror.True)

	// ErrQueueFull is returned when the action queue is at capacity.
	ErrQueueFull = apierror.New(apierror.RateLimited, "pg: action queue is full").WithRetryable(apierror.True)

	// ErrBroadcastTimeout is returned when a broadcast operation times out.
	ErrBroadcastTimeout = apierror.New(apierror.Timeout, "pg: broadcast timeout").WithRetryable(apierror.True)

	// ErrCircuitOpen is returned when attempting to send to a node with an open circuit breaker.
	ErrCircuitOpen = apierror.New(apierror.Unavailable, "pg: circuit breaker is open for target node").WithRetryable(apierror.True)
)
