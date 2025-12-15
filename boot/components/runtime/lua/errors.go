package lua

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDispatcherNotFound          = apierror.New(apierror.KindInternal, "dispatcher not found in context").WithRetryable(apierror.False)
	ErrDispatcherRegistrarNotFound = apierror.New(apierror.KindInternal, "dispatcher registrar not found in context").WithRetryable(apierror.False)
)
