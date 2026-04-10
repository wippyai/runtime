// SPDX-License-Identifier: MPL-2.0

package pg

import apierror "github.com/wippyai/runtime/api/error"

var (
	// ErrInvalidActionQueueSize is returned when action_queue_size is negative.
	ErrInvalidActionQueueSize = apierror.New(apierror.Invalid, "pg: action queue size must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMonitorBuffer is returned when monitor_buffer is negative.
	ErrInvalidMonitorBuffer = apierror.New(apierror.Invalid, "pg: monitor buffer must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMaxGroups is returned when max_groups is negative.
	ErrInvalidMaxGroups = apierror.New(apierror.Invalid, "pg: max groups must be non-negative").WithRetryable(apierror.False)

	// ErrInvalidMaxMembersPerGroup is returned when max_members_per_group is negative.
	ErrInvalidMaxMembersPerGroup = apierror.New(apierror.Invalid, "pg: max members per group must be non-negative").WithRetryable(apierror.False)

	// ErrMaxGroupsReached is returned when joining would exceed the max_groups limit.
	ErrMaxGroupsReached = apierror.New(apierror.RateLimited, "pg: maximum number of groups reached").WithRetryable(apierror.True)

	// ErrMaxMembersReached is returned when joining would exceed the max_members_per_group limit.
	ErrMaxMembersReached = apierror.New(apierror.RateLimited, "pg: maximum members per group reached").WithRetryable(apierror.True)
)
