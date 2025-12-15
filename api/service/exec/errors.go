package exec

import apierror "github.com/wippyai/runtime/api/error"

var ErrImageRequired = apierror.New(apierror.KindInvalid, "docker image is required").WithRetryable(apierror.False)
