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

	// ErrPendingConflict is returned when a ROOT-scope register attempt
	// targets a name that is already in the pending state for a different
	// PID. The reservation must complete (active or expired) before the
	// next pending attempt is accepted.
	ErrPendingConflict = apierror.New(apierror.AlreadyExists, "root name reservation pending for different PID").WithRetryable(apierror.False)

	// ErrRootRegistrationTimeout is returned when a ROOT-scope register
	// failed to collect an ack from every live node in the membership
	// snapshot before its deadline. The error carries the list of missing
	// node IDs via RootRegistrationTimeoutError so callers can pinpoint
	// the offender.
	ErrRootRegistrationTimeout = apierror.New(apierror.Timeout, "root registration timed out before all live nodes acked").WithRetryable(apierror.True)
)

// RootRegistrationTimeoutError wraps ErrRootRegistrationTimeout with the
// set of nodes that failed to ack in time. Use errors.As(err, &target) on
// the error returned by Register(ctx, name, pid, Root) to read it.
type RootRegistrationTimeoutError struct {
	Name        string
	MissingAcks []string
	Epoch       uint64
}

// Error makes the timeout satisfy the error interface.
func (e *RootRegistrationTimeoutError) Error() string {
	return ErrRootRegistrationTimeout.Error() + " (name=" + e.Name + ")"
}

// Unwrap exposes the sentinel so errors.Is(err, ErrRootRegistrationTimeout)
// works on a returned RootRegistrationTimeoutError.
func (e *RootRegistrationTimeoutError) Unwrap() error { return ErrRootRegistrationTimeout }
