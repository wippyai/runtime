// SPDX-License-Identifier: MPL-2.0

package toolchain

import (
	apierror "github.com/wippyai/runtime/api/error"
)

// NewExecutableError creates an error when the running executable cannot be read.
func NewExecutableError(cause error) apierror.Error {
	return apierror.New(apierror.Internal, "cannot determine running executable identity").
		WithRetryable(apierror.False).
		WithCause(cause)
}
