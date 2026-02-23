// SPDX-License-Identifier: MPL-2.0

package queue

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrLoggerNotAvailable   = apierror.New(apierror.Internal, "logger not available in context").WithRetryable(apierror.False)
	ErrEventBusNotAvailable = apierror.New(apierror.Internal, "event bus not available in context").WithRetryable(apierror.False)
	ErrRegistryNotAvailable = apierror.New(apierror.Internal, "registry not available in context").WithRetryable(apierror.False)
)
