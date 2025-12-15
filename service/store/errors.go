package store

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrStoreFull   = apierror.New(apierror.Unavailable, "store is full").WithRetryable(apierror.True)
	ErrStoreClosed = apierror.New(apierror.Unavailable, "store is closed").WithRetryable(apierror.False)
)
