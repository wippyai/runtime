package dispatchers

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDispatcherNotFound = apierror.New(apierror.Internal, "dispatcher registrar not found in context").WithRetryable(apierror.False)
)
