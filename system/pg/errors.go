// SPDX-License-Identifier: MPL-2.0

package pg

import apierror "github.com/wippyai/runtime/api/error"

var (
	// ErrNotJoined is returned when a process tries to leave a group it hasn't joined.
	ErrNotJoined = apierror.New(apierror.NotFound, "pg: process not joined in group").WithRetryable(apierror.False)

	// ErrGroupNotFound is returned when querying a non-existent group.
	ErrGroupNotFound = apierror.New(apierror.NotFound, "pg: group not found").WithRetryable(apierror.False)

	// ErrServiceStopped is returned when the pg service is not running.
	ErrServiceStopped = apierror.New(apierror.Unavailable, "pg: service stopped").WithRetryable(apierror.False)
)
