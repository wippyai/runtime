// SPDX-License-Identifier: MPL-2.0

package exec

import apierror "github.com/wippyai/runtime/api/error"

// ErrImageRequired indicates a missing container image.
var ErrImageRequired = apierror.New(apierror.Invalid, "docker image is required").WithRetryable(apierror.False)

var ErrPTYUnavailable = apierror.New(apierror.Unavailable, "PTY is unavailable").WithRetryable(apierror.False)

var ErrInvalidPTYSize = apierror.New(apierror.Invalid, "PTY dimensions must be between 1 and 65535").WithRetryable(apierror.False)
