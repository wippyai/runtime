// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	apierror "github.com/wippyai/runtime/api/error"
)

const (
	Terminated apierror.Kind = "Terminated"
	Exited     apierror.Kind = "Exited"
)

var (
	ErrTerminated         = apierror.New(Terminated, "service terminated").WithRetryable(apierror.False)
	ErrExit               = apierror.New(Exited, "service exited").WithRetryable(apierror.False)
	ErrOutsideTransaction = apierror.New(apierror.Invalid, "action received outside of transaction").WithRetryable(apierror.False)
)
