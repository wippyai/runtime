// SPDX-License-Identifier: MPL-2.0

package attempt

import apierror "github.com/wippyai/runtime/api/error"

// NewStaleIncarnationError reports an executor fenced out by a newer actor
// incarnation; the losing attempt must exit without retrying.
func NewStaleIncarnationError(opID string) apierror.Error {
	return apierror.New(apierror.Conflict, "stale incarnation for operation: "+opID).WithRetryable(apierror.False)
}

// NewOperationFailedError reports a terminally failed operation. The failure
// is persisted; retrying the activity cannot change the outcome.
func NewOperationFailedError(opID, detail string) apierror.Error {
	return apierror.New(apierror.Internal, "operation failed: "+opID+": "+detail).WithRetryable(apierror.False)
}

// NewGateTimeoutError reports a dispatch gate that never became satisfied
// within the gate timeout (design §9.4 step 5).
func NewGateTimeoutError(opID, detail string) apierror.Error {
	return apierror.New(apierror.Timeout, "gate timeout for operation "+opID+": "+detail).WithRetryable(apierror.False)
}

// NewActorExitError reports a provider actor that exited without a terminal
// signal. Retryable: the next activity attempt bumps the incarnation and
// respawns the actor from its checkpoint (design §9.7, PoC subset).
func NewActorExitError(opID string) apierror.Error {
	return apierror.New(apierror.Unavailable, "provider actor exited without terminal signal: "+opID).WithRetryable(apierror.True)
}

// NewOperationTimeoutError reports an operation that exceeded its deadline.
func NewOperationTimeoutError(opID string) apierror.Error {
	return apierror.New(apierror.Timeout, "operation timed out: "+opID).WithRetryable(apierror.False)
}

// NewIllegalSignalError reports a signal violating the closed FSM; it is a
// provider bug surfaced non-retryable (design §5.1).
func NewIllegalSignalError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "illegal signal: "+detail).WithRetryable(apierror.False)
}

// NewMissingOutputError reports a committed producer without the referenced
// output key.
func NewMissingOutputError(resourceID, key string) apierror.Error {
	return apierror.New(apierror.Invalid, "resource "+resourceID+" has no output "+key).WithRetryable(apierror.False)
}
