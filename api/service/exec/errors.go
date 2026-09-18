// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

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

// NewInvalidMountError reports a mount that cannot be bound, keeping
// ErrInvalidMount as the cause so callers match the condition.
func NewInvalidMountError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid process mount: "+detail).
		WithRetryable(apierror.False).
		WithCause(ErrInvalidMount)
}

// NewDuplicateMountTargetError reports two mounts claiming one container path.
func NewDuplicateMountTargetError(target string) apierror.Error {
	return apierror.New(apierror.Invalid, "duplicate process mount target: "+target).
		WithRetryable(apierror.False).
		WithDetails(attrs.NewBagFrom(map[string]any{"target": target})).
		WithCause(ErrDuplicateMountTarget)
}
