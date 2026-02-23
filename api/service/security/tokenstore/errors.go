// SPDX-License-Identifier: MPL-2.0

package tokenstore

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrStoreIDRequired           = apierror.New(apierror.Invalid, "store ID is required").WithRetryable(apierror.False)
	ErrTokenLengthMustBePositive = apierror.New(apierror.Invalid, "token length must be positive").WithRetryable(apierror.False)
)
