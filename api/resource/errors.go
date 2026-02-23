// SPDX-License-Identifier: MPL-2.0

package resource

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrNotFound = apierror.New(apierror.NotFound, "resource not found").WithRetryable(apierror.False)
	ErrReleased = apierror.New(apierror.Invalid, "resource has been released").WithRetryable(apierror.False)
)
