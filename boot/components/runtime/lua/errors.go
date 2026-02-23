// SPDX-License-Identifier: MPL-2.0

package lua

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrDispatcherNotFound          = apierror.New(apierror.Internal, "dispatcher not found in context").WithRetryable(apierror.False)
	ErrDispatcherRegistrarNotFound = apierror.New(apierror.Internal, "dispatcher registrar not found in context").WithRetryable(apierror.False)
	ErrCodeManagerNotFound         = apierror.New(apierror.Internal, "code manager not found in context").WithRetryable(apierror.False)
)
