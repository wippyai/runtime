// SPDX-License-Identifier: MPL-2.0

package context

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Errors returned by context operations.
var (
	ErrNoFrameContext = apierror.New(apierror.Invalid, "no frame context available").WithRetryable(apierror.False)
	ErrNoAppContext   = apierror.New(apierror.Invalid, "no app context available").WithRetryable(apierror.False)
	ErrFrameSealed    = apierror.New(apierror.Invalid, "frame is sealed").WithRetryable(apierror.False)
)

// NewFrameSealedError creates an error for attempting to set a key in a sealed frame.
func NewFrameSealedError(key any) apierror.Error {
	details := attrs.NewBag()
	details.Set("key", key)
	return apierror.E(
		apierror.Invalid,
		"cannot set key in sealed frame",
		apierror.False,
		details,
		nil,
	)
}
