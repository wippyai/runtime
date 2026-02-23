// SPDX-License-Identifier: MPL-2.0

package consumer

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrQueueIDRequired    = apierror.New(apierror.Invalid, "queue ID is required").WithRetryable(apierror.False)
	ErrFunctionIDRequired = apierror.New(apierror.Invalid, "function ID is required").WithRetryable(apierror.False)
)
