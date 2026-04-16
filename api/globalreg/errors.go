// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for global registry operations.
var (
	// ErrNameAlreadyRegistered is returned when a name is already taken by a different PID.
	ErrNameAlreadyRegistered = apierror.New(apierror.AlreadyExists, "global name already registered").WithRetryable(apierror.False)

	// ErrNameNotFound is returned when a name is not in the global registry.
	ErrNameNotFound = apierror.New(apierror.NotFound, "global name not found").WithRetryable(apierror.False)

	// ErrNotAvailable is returned when the global registry is not available
	// (e.g., no Raft leader, or registry not initialized).
	ErrNotAvailable = apierror.New(apierror.Unavailable, "global registry not available").WithRetryable(apierror.True)

	// ErrStaleFence is returned when a fencing token is older than the current
	// registration. This means the name was re-registered after the caller
	// looked it up — the caller should re-lookup and retry.
	ErrStaleFence = apierror.New(apierror.Conflict, "stale fencing token: name has been re-registered").WithRetryable(apierror.True)

	// ErrNotReady is returned when the node has not yet caught up with the
	// Raft log and cannot serve consistent lookups.
	ErrNotReady = apierror.New(apierror.Unavailable, "global registry not ready: node is catching up").WithRetryable(apierror.True)
)
