// SPDX-License-Identifier: MPL-2.0

package ledger

import apierror "github.com/wippyai/runtime/api/error"

// NewDeployNotFoundError reports a missing deploy row.
func NewDeployNotFoundError(deployID string) apierror.Error {
	return apierror.New(apierror.NotFound, "deploy not found: "+deployID).WithRetryable(apierror.False)
}

// NewPlanNotFoundError reports a missing plan row.
func NewPlanNotFoundError(planID string) apierror.Error {
	return apierror.New(apierror.NotFound, "plan not found: "+planID).WithRetryable(apierror.False)
}

// NewOperationNotFoundError reports a missing operation row.
func NewOperationNotFoundError(key string) apierror.Error {
	return apierror.New(apierror.NotFound, "operation not found: "+key).WithRetryable(apierror.False)
}

// NewSpecNotFoundError reports a missing resource spec row.
func NewSpecNotFoundError(deployID, resourceID string) apierror.Error {
	return apierror.New(apierror.NotFound, "spec not found: "+deployID+"/"+resourceID).WithRetryable(apierror.False)
}

// NewInstanceNotFoundError reports a missing resource instance row.
func NewInstanceNotFoundError(resourceID string) apierror.Error {
	return apierror.New(apierror.NotFound, "resource instance not found: "+resourceID).WithRetryable(apierror.False)
}

// NewIllegalTransitionError reports a state transition outside the closed
// automata; it is a bug, surfaced non-retryable, never silently absorbed.
func NewIllegalTransitionError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "illegal transition: "+detail).WithRetryable(apierror.False)
}

// NewCommitFencedError reports a terminal commit rejected by the incarnation
// or status fence; the caller must re-read and resolve.
func NewCommitFencedError(opID string) apierror.Error {
	return apierror.New(apierror.Conflict, "commit fenced out: "+opID).WithRetryable(apierror.False)
}

// NewResourceBusyError reports a dispatch blocked by the one-active-operation
// -per-resource invariant (design I7): another operation on the same resource
// is live, so this dispatch is retryable and waits its turn.
func NewResourceBusyError(opID string) apierror.Error {
	return apierror.New(apierror.Conflict, "another operation is active on this resource: "+opID).WithRetryable(apierror.True)
}
