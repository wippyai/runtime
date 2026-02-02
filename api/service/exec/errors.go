package exec

import apierror "github.com/wippyai/runtime/api/error"

// ErrImageRequired indicates a missing container image.
var ErrImageRequired = apierror.New(apierror.Invalid, "docker image is required").WithRetryable(apierror.False)
