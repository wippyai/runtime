package resource

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrNotFound = apierror.New(apierror.KindNotFound, "resource not found").WithRetryable(apierror.False)
	ErrReleased = apierror.New(apierror.KindInvalid, "resource has been released").WithRetryable(apierror.False)
)
