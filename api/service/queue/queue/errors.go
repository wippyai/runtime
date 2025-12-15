package queue

import apierror "github.com/wippyai/runtime/api/error"

var ErrDriverIDRequired = apierror.New(apierror.KindInvalid, "driver ID is required").WithRetryable(apierror.False)
