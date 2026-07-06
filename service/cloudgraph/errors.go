// SPDX-License-Identifier: MPL-2.0

package cloudgraph

import apierror "github.com/wippyai/runtime/api/error"

// Manager-level failures.
var (
	ErrResourceRegistryMissing = apierror.New(apierror.Internal, "resource registry not available").WithRetryable(apierror.False)
	ErrInvalidDatabaseResource = apierror.New(apierror.Invalid, "database entry is not a SQL resource").WithRetryable(apierror.False)
	ErrInvalidClientResource   = apierror.New(apierror.Invalid, "client entry is not a temporal client resource").WithRetryable(apierror.False)
	ErrEngineNotReady          = apierror.New(apierror.Unavailable, "cloudgraph engine not ready").WithRetryable(apierror.True)
)

// NewEngineConflictError reports a second cloudgraph.engine entry; the PoC
// runs exactly one engine per installation.
func NewEngineConflictError(id string) apierror.Error {
	return apierror.New(apierror.Conflict, "cloudgraph engine already loaded, rejecting: "+id).WithRetryable(apierror.False)
}

// NewInvalidDeployInputError reports a malformed deploy submission.
func NewInvalidDeployInputError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid deploy input: "+detail).WithRetryable(apierror.False)
}

// NewWorkerNotFoundError reports an engine referencing an unknown worker.
func NewWorkerNotFoundError(id string) apierror.Error {
	return apierror.New(apierror.NotFound, "temporal worker not found: "+id).WithRetryable(apierror.False)
}
