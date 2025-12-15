package resource

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for resource operations.
var (
	ErrNotFound = apierror.New(apierror.KindNotFound, "resource not found").WithRetryable(apierror.False)
	ErrLocked   = apierror.New(apierror.KindUnavailable, "resource is locked").WithRetryable(apierror.True)
	ErrReleased = apierror.New(apierror.KindInvalid, "resource has been released").WithRetryable(apierror.False)
	ErrClosed   = apierror.New(apierror.KindUnavailable, "resource provider is closed").WithRetryable(apierror.False)
	ErrInUse    = apierror.New(apierror.KindUnavailable, "resource is in use").WithRetryable(apierror.True)
)
