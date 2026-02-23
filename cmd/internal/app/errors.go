// SPDX-License-Identifier: MPL-2.0

package app

import (
	apierror "github.com/wippyai/runtime/api/error"
)

func NewCreateLoggerError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create logger").WithCause(cause).WithRetryable(apierror.False)
}
