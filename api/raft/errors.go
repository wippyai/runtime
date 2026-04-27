// SPDX-License-Identifier: MPL-2.0

package raft

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for Raft operations.
var (
	// ErrNotLeader is returned when a write operation is attempted on a non-leader node.
	ErrNotLeader = apierror.New(apierror.Unavailable, "not the raft leader").WithRetryable(apierror.True)

	// ErrNoLeader is returned when no leader is currently known.
	ErrNoLeader = apierror.New(apierror.Unavailable, "no raft leader known").WithRetryable(apierror.True)

	// ErrLeadershipLost is returned when leadership is lost during an operation.
	ErrLeadershipLost = apierror.New(apierror.Unavailable, "leadership lost during operation").WithRetryable(apierror.True)

	// ErrTimeout is returned when a Raft operation times out.
	ErrTimeout = apierror.New(apierror.Timeout, "raft operation timed out").WithRetryable(apierror.True)

	// ErrNodeExists is returned when trying to add a node that already exists.
	ErrNodeExists = apierror.New(apierror.AlreadyExists, "raft node already exists").WithRetryable(apierror.False)

	// ErrNotRunning is returned when the Raft node is not running.
	ErrNotRunning = apierror.New(apierror.Unavailable, "raft node not running").WithRetryable(apierror.True)

	// ErrServerNotFound is returned when the requested server ID does not
	// exist in the current Raft configuration.
	ErrServerNotFound = apierror.New(apierror.NotFound, "raft server not found").WithRetryable(apierror.False)
)
