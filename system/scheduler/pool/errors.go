package pool

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var ErrPoolClosed = apierror.New(apierror.KindUnavailable, "pool is closed").WithRetryable(apierror.False)
