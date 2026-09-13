// SPDX-License-Identifier: MPL-2.0

package exec

import apierror "github.com/wippyai/runtime/api/error"

// ErrImageRequired indicates a missing container image.
var ErrImageRequired = apierror.New(apierror.Invalid, "docker image is required").WithRetryable(apierror.False)

var ErrPTYUnavailable = apierror.New(apierror.Unavailable, "PTY is unavailable").WithRetryable(apierror.False)

var ErrInvalidPTYSize = apierror.New(apierror.Invalid, "PTY dimensions must be positive, at most 65535, and within the cell limit").WithRetryable(apierror.False)

var ErrCommandRequired = apierror.New(apierror.Invalid, "command is required").WithRetryable(apierror.False)

var ErrProcessGroupUnsupported = apierror.New(apierror.Unavailable, "process groups are not supported on this platform").WithRetryable(apierror.False)

var ErrInvalidCommand = apierror.New(apierror.Invalid, "invalid command").WithRetryable(apierror.False)

var ErrInvalidMount = apierror.New(apierror.Invalid, "invalid process mount").WithRetryable(apierror.False)

var ErrDuplicateMountTarget = apierror.New(apierror.Invalid, "duplicate process mount target").WithRetryable(apierror.False)

var ErrMountsUnsupported = apierror.New(apierror.Unavailable, "process mounts are not supported by this executor").WithRetryable(apierror.False)
