// SPDX-License-Identifier: MPL-2.0

package function

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrRegistryNotFound = apierror.New(apierror.NotFound, "function registry not found in context").WithRetryable(apierror.False)

	ErrProcessContextNotFound = apierror.New(apierror.NotFound, "process context not found").WithRetryable(apierror.False)

	ErrCallNotFound = apierror.New(apierror.NotFound, "async call not found").WithRetryable(apierror.False)

	ErrNilContext = apierror.New(apierror.Invalid, "nil context").WithRetryable(apierror.False)

	ErrNilCallback = apierror.New(apierror.Invalid, "nil callback").WithRetryable(apierror.False)

	ErrNodeNotFound = apierror.New(apierror.NotFound, "relay node not configured").WithRetryable(apierror.False)

	ErrPIDNotFound = apierror.New(apierror.NotFound, "frame PID not found in context").WithRetryable(apierror.False)

	ErrPIDGeneratorNotFound = apierror.New(apierror.NotFound, "PID generator not found in context").WithRetryable(apierror.False)
)
