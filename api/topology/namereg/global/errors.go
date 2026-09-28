// SPDX-License-Identifier: MPL-2.0

package global

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

	// ErrNotReady is returned when the node has not yet caught up with the
	// Raft log and cannot serve consistent lookups.
	ErrNotReady = apierror.New(apierror.Unavailable, "global registry not ready: node is catching up").WithRetryable(apierror.True)

	// ErrPendingConflict is returned when a Strong-scope register attempt
	// targets a name that is already in the pending state for a different
	// PID. The reservation must complete (active or expired) before the
	// next pending attempt is accepted.
	ErrPendingConflict = apierror.New(apierror.AlreadyExists, "strong name reservation pending for different PID").WithRetryable(apierror.False)

	// ErrStrongRegistrationTimeout is returned when a Strong-scope register
	// failed to collect an ack from every observer captured for that attempt
	// before its deadline. StrongRegistrationTimeoutError lists the missing
	// observer IDs.
	ErrStrongRegistrationTimeout = apierror.New(apierror.Timeout, "strong registration timed out before all observers acked").WithRetryable(apierror.True)

	// ErrStrongRegistrationRejected is returned when a required observer rejected
	// a Strong-scope register. Distinct from a
	// timeout: the registration failed terminally and is not retryable.
	ErrStrongRegistrationRejected = apierror.New(apierror.AlreadyExists, "strong registration rejected by a required node").WithRetryable(apierror.False)
)

// StrongRegistrationTimeoutError wraps ErrStrongRegistrationTimeout with the
// set of nodes that failed to ack in time. Use errors.As(err, &target) on
// the error returned by Register(ctx, name, pid, Strong) to read it.
type StrongRegistrationTimeoutError struct {
	Name        string
	MissingAcks []string
	Epoch       uint64
}

// Error makes the timeout satisfy the error interface.
func (e *StrongRegistrationTimeoutError) Error() string {
	return ErrStrongRegistrationTimeout.Error() + " (name=" + e.Name + ")"
}

// Unwrap exposes the sentinel so errors.Is(err, ErrStrongRegistrationTimeout)
// works on a returned StrongRegistrationTimeoutError.
func (e *StrongRegistrationTimeoutError) Unwrap() error { return ErrStrongRegistrationTimeout }

// StrongConflictError wraps ErrStrongRegistrationRejected with the node that
// rejected the reservation and the reject reason. Returned by
// Register(ctx, name, pid, Strong) when a required node NACKs the open. Use
// errors.As(err, &target) to read it; it is distinct from
// StrongRegistrationTimeoutError so callers can tell a conflict from a timeout.
type StrongConflictError struct {
	Name       string
	Reason     string
	RejectedBy string
	Epoch      uint64
}

// Error makes the conflict satisfy the error interface.
func (e *StrongConflictError) Error() string {
	return ErrStrongRegistrationRejected.Error() + " (name=" + e.Name + " by=" + e.RejectedBy + ")"
}

// Unwrap exposes the sentinel so errors.Is(err, ErrStrongRegistrationRejected)
// works on a returned StrongConflictError.
func (e *StrongConflictError) Unwrap() error { return ErrStrongRegistrationRejected }
